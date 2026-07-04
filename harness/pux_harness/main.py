"""In-process deepagents runner — drive any org against the harness directly.

  uv run python -m pux_harness.main --list                       # discovered orgs + agents
  uv run python -m pux_harness.main --check                      # docker-exec + native specialist smoke (no tokens)
  uv run python -m pux_harness.main --org general                # default forcing task
  uv run python -m pux_harness.main --org dev-bot --task "..."   # custom task

Proves per-org: deepagents drives the pux sandbox through ``PuxSandboxBackend``
(native ``ls/read_file/write_file/edit_file/glob/grep/execute`` via docker
exec), the CTO delegates to its specialist via ``task(subagent_type=...)``, and
specialists fall back to those same native tools. The 13 specialist capabilities
(browser/desktop/vision/skills/python) are native ``pux_sandbox_*`` Python tools
too — there is no Go bridge. Prints a message trace + token usage so
cost/output is comparable.
"""
from __future__ import annotations

import argparse
import asyncio
import os
import uuid

from langgraph.checkpoint.memory import MemorySaver

from pux_harness.agent.contract import (
    check_all,
    check_harness,
    check_skill_roots,
    has_errors,
)
from pux_harness.sandbox.docker_exec import get_exec_client
from pux_harness.agent.graph import build_graph, shared_backend
from pux_harness.sandbox.tools import build_native_specialists
from pux_harness.agent.orgs import (
    discover_orgs,
    org_agent_slugs,
)
from pux_harness.sandbox.backend import PuxSandboxBackend

# Per-org default forcing tasks. Each is UN-answerable from the system prompt
# alone, so the agent must delegate to its specialist — the seam we're proving.
# Tasks name the NATIVE `execute` tool (Phase 3); `pux_sandbox_bash` is gone.
DEFAULT_TASKS: dict[str, str] = {
    "general": (
        "How many Python modules ship under /sandbox/workspace/harness/pux_harness/, "
        "and what are their names? Delegate to the `researcher` subagent — do NOT "
        "inspect the codebase yourself. Have it run, via the native `execute` tool: "
        "`find /sandbox/workspace/harness/pux_harness -name '*.py'`. "
        "Report the researcher's findings verbatim."
    ),
    "_demo": (
        "List the top-level entries of the project root inside the sandbox. Delegate "
        "to the `researcher` subagent — do NOT run tools yourself. Have it use the "
        "native `execute` tool: `ls -1 /sandbox/workspace`. Report its findings verbatim."
    ),
    # `go` is not installed in the pux-sandbox image, so we exercise the
    # read-only explorer specialist rather than the tester's run-tests path.
    "dev-bot": (
        "What does the dev-bot sample Go package export? Delegate to the "
        "`dev-bot-explorer` subagent — do NOT inspect the code yourself. The package "
        "is under /sandbox/workspace/orgs/dev-bot/. Have the explorer find every "
        "exported identifier (names starting with an uppercase letter) and report "
        "each with a file:line citation. Report its findings verbatim."
    ),
    # --- Phase 5: the remaining 7 orgs. Each forces delegation to a named
    # specialist and drives a NATIVE tool against the org's OWN bundled content
    # (no external keys/images needed). Answers are verifiable against the FS.
    "invest": (
        "How many Python modules are under /sandbox/workspace/orgs/invest/sandbox/? "
        "Delegate to the `invest-researcher` subagent — do NOT inspect the code "
        "yourself. Have it run, via the native `execute` tool: "
        "`find /sandbox/workspace/orgs/invest/sandbox -name '*.py'`. "
        "Report the count and the module filenames verbatim."
    ),
    "game-studio": (
        "What playbook markdown docs ship under /sandbox/workspace/orgs/game-studio/skills/? "
        "Delegate to the `game-studio-docs-writer` subagent — do NOT look yourself. "
        "Have it use the native `glob` tool for "
        "`/sandbox/workspace/orgs/game-studio/skills/*.md` and list each filename. "
        "Report verbatim."
    ),
    "deep-research-engine": (
        "How many Python modules are under /sandbox/workspace/orgs/deep-research-engine/sandbox/? "
        "Delegate to the `dre-auditor` subagent — do NOT inspect yourself. Have it "
        "run, via the native `execute` tool: "
        "`find /sandbox/workspace/orgs/deep-research-engine/sandbox -name '*.py'`. "
        "Report the count and module filenames verbatim."
    ),
    "social-media-pipeline": (
        "Read the campaign-angles file at "
        "/sandbox/workspace/orgs/social-media-pipeline/data/options.json. Delegate to "
        "the `smp-writer` subagent — do NOT read it yourself. Have it use the native "
        "`read_file` tool, then report how many angles there are and the id + angle "
        "of each. Report verbatim."
    ),
    "twitter-agent": (
        "What helper docs ship under /sandbox/workspace/orgs/twitter-agent/skills/? "
        "Delegate to the `twitter-drafter` subagent — do NOT look yourself. Have it "
        "use the native `glob` tool for `/sandbox/workspace/orgs/twitter-agent/skills/**`. "
        "Report the filenames found."
    ),
    "telegram-agent": (
        "Read the campaign file at /sandbox/workspace/orgs/telegram-agent/data/campaign.json. "
        "Delegate to the `telegram-drafter` subagent — do NOT read it yourself. Have it "
        "use the native `read_file` tool, then report how many messages the campaign "
        "contains. Report the count verbatim."
    ),
    "video-production": (
        "What ships under /sandbox/workspace/orgs/video-production/skills/? Delegate to "
        "the `video-scriptwriter` subagent — do NOT look yourself. Have it use the "
        "native `execute` tool: `ls -1 /sandbox/workspace/orgs/video-production/skills`. "
        "Report the entries verbatim."
    ),
}


def _build_agent(org: str):
    # Ephemeral in-memory checkpointer — the runner is one-shot per process.
    # The server (server.py) uses a persistent AsyncSqliteSaver instead.
    agent = build_graph(org, checkpointer=MemorySaver())
    return agent, shared_backend()


def _usage(messages: list) -> dict:
    tot = {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
    for msg in messages:
        meta = getattr(msg, "usage_metadata", None) or {}
        for k in tot:
            tot[k] += meta.get(k, 0) or 0
    return tot


def _trace(messages: list) -> None:
    """One line per message so delegation + tool calls are honestly visible."""
    for i, m in enumerate(messages):
        t = getattr(m, "type", type(m).__name__)
        name = getattr(m, "name", "") or ""
        tool_calls = getattr(m, "tool_calls", None) or []
        content = getattr(m, "content", "")
        if isinstance(content, list):
            content = " | ".join(str(c)[:140] for c in content)
        cstr = str(content).replace("\n", " ")[:200]
        tcstr = ""
        if tool_calls:
            parts = []
            for tc in tool_calls:
                tcname = tc.get("name") if isinstance(tc, dict) else getattr(tc, "name", "?")
                tcargs = tc.get("args") if isinstance(tc, dict) else getattr(tc, "args", {})
                parts.append(f"{tcname}({tcargs})")
            tcstr = " TOOLS=[" + "; ".join(parts)[:240] + "]"
        tag = f":{name}" if name else ""
        print(f"  [{i}] {t}{tag}{tcstr}: {cstr}")


async def _run(org: str, task: str, recursion_limit: int) -> None:
    agent, backend = _build_agent(org)
    print(f"[org] {org}   [task] {task}\n")
    result = await agent.ainvoke(
        {"messages": [{"role": "user", "content": task}]},
        config={
            "configurable": {"thread_id": f"{org}-{uuid.uuid4().hex[:8]}"},
            "recursion_limit": recursion_limit,
        },
    )
    messages = result["messages"]
    print("=== MESSAGE TRACE ===")
    _trace(messages)
    print("\n=== FINAL ANSWER ===")
    final = messages[-1]
    content = getattr(final, "content", final)
    print(content if content else "(empty content)")
    print("\n=== USAGE ===")
    print(_usage(messages))
    print(f"messages in thread: {len(messages)}")

    # Top-level surface check: only the MAIN agent's tool_calls live in
    # `messages` — subagent calls run in a nested thread the main trace can't
    # see. So this proves the CTO didn't leak a legacy tool, but the real
    # native-flip proof for the subagent is `backend.execute_log` below.
    native = {"ls", "read_file", "write_file", "edit_file", "glob", "grep", "execute"}
    legacy = {"pux_sandbox_bash", "pux_sandbox_file_read", "pux_sandbox_file_write",
              "pux_sandbox_file_edit", "pux_sandbox_file_glob", "pux_sandbox_file_grep"}
    used: set[str] = set()
    for m in messages:
        for tc in (getattr(m, "tool_calls", None) or []):
            used.add(tc.get("name") if isinstance(tc, dict) else getattr(tc, "name", ""))
    leaked = used & legacy
    print(f"\n=== SURFACE CHECK (main agent only) ===")
    print(f"  tools used: {sorted(used)}")
    print(f"  native fs used: {sorted(used & native) or 'NONE'}")
    print(f"  legacy pux_sandbox fs/shell leaked: {sorted(leaked) or 'NONE'}")

    # Native-flip proof across the WHOLE tree: every native fs/shell call (the
    # main agent's, AND every subagent's) routes through backend.execute() —
    # incl. the inherited ls/read/glob/grep/write/edit. pux_sandbox_bash is
    # never bound, so ANY entry here is a native call by construction. Empty
    # would mean the tree did no shell/fs work at all (it delegated and the
    # specialist answered from memory only) — for these forcing tasks that's
    # a red flag, not a pass.
    print(f"\n=== NATIVE EXECUTE LOG (whole agent tree, {len(backend.execute_log)} calls) ===")
    for cmd in backend.execute_log:
        one = " ".join(cmd.split())
        print(f"  $ {one[:140]}")
    if not backend.execute_log:
        print("  (none — no native fs/shell call was made this run)")


def _jobs_run(org: str, job: str | None) -> int:
    """Run prep jobs inside the org's sandbox container (Phase 14)."""
    from pux_harness.sandbox.container import SandboxContainer  # noqa: PLC0415
    from pux_harness.sandbox.docker_exec import DockerExecClient  # noqa: PLC0415
    from pux_harness.sandbox.jobs import run_jobs  # noqa: PLC0415
    from pux_harness.sandbox import policy as policy_mod  # noqa: PLC0415
    from pux_harness.agent.orgs import PROJECT_ROOT  # noqa: PLC0415

    try:
        pol = policy_mod.load(org, PROJECT_ROOT)
    except policy_mod.NoPolicy:
        print(f"{org}: no policy.yaml — no jobs declared")
        return 0

    specs = policy_mod.job_specs(pol)
    if not specs:
        print(f"{org}: no jobs declared")
        return 0

    if job:
        specs = [s for s in specs if s.name == job]
        if not specs:
            print(f"{org}: no job named {job!r}")
            return 1

    print(f"[jobs] {org}: running {len(specs)} job(s)")
    sb = SandboxContainer(org=org)
    container_name = sb.ensure()
    ec = DockerExecClient(container=container_name)
    results = run_jobs(pol, ec)

    if job:
        results = [r for r in results if r.name == job]

    for r in results:
        icon = "ok" if r.status == "ok" else "FAIL"
        err = f"  error={r.error[:120]}" if r.error else ""
        print(f"  {r.name:<24} {icon:<6} {r.duration:.1f}s{err}")

    failed = [r for r in results if r.status != "ok"]
    print(f"\n{len(results)} jobs run, {len(failed)} failed")
    return 1 if failed else 0


def _jobs_status(org: str) -> int:
    """Show declared prep jobs for this org (Phase 14)."""
    from pux_harness.sandbox import policy as policy_mod  # noqa: PLC0415
    from pux_harness.agent.orgs import PROJECT_ROOT  # noqa: PLC0415

    try:
        pol = policy_mod.load(org, PROJECT_ROOT)
    except policy_mod.NoPolicy:
        print(f"{org}: no policy.yaml — no jobs declared")
        return 0

    specs = policy_mod.job_specs(pol)
    if not specs:
        print(f"{org}: no jobs declared")
        return 0

    print(f"{'NAME':<24} {'SCRIPT':<40} {'TIMEOUT':<8} DESCRIPTION")
    for s in specs:
        timeout = f"{s.timeout}s" if s.timeout else "none"
        print(f"  {s.name:<22} {s.script:<40} {timeout:<8} {s.description}")
    return 0


def _check_policy(org: str) -> int:
    """Resolve + report an org's policy WITHOUT running the model — a dry-run of
    what container-side enforcement (Phase 8) will do. Prints expanded mounts,
    credential presence (names only — never values; ``.env`` holds live keys),
    the rendered egress allowlist (DNS resolved now), and tier/image overrides.

    Exits 1 if any required credential is missing — the same gate a real
    container create would enforce, so this is a usable pre-flight."""
    from pux_harness.sandbox import policy
    from pux_harness.agent.orgs import PROJECT_ROOT

    try:
        p = policy.load(org, PROJECT_ROOT)
    except policy.NoPolicy:
        print(f"{org}: no policy.yaml — today's behavior "
              "(full egress, default image/tier, no required creds).")
        return 0

    print(f"## {org} policy")
    mounts = policy.resolve_mounts(p)
    if mounts:
        print("workspace.mounts:")
        for m in mounts:
            print(f"  {m.host} -> {m.container} ({m.mode})")
    else:
        print("workspace.mounts: (none)")

    present = [n for n in p.credentials.required if os.environ.get(n, "")]
    missing = [n for n in p.credentials.required if not os.environ.get(n, "")]
    opt_present = [n for n in p.credentials.optional if os.environ.get(n, "")]
    print(f"credentials.required: {p.credentials.required or '(none)'}")
    print(f"  present:  {present or '(none)'}")
    print(f"  MISSING:  {missing or '(none)'}")
    print(f"credentials.optional present: {opt_present or '(none)'}")

    if p.egress.allow:
        try:
            rules = policy.egress_rules(p)
            print("egress.allow (DNS-resolved now):")
            for line in rules.rstrip("\n").split("\n"):
                print(f"  {line}")
        except policy.PolicyError as e:
            print(f"egress.allow: RESOLUTION ERROR — {e}")
            missing = missing or ["<egress-unresolvable>"]  # fail the gate
    else:
        print("egress.allow: (none — full egress)")

    print(f"sandbox.image: {p.sandbox.image or '(default pux-sandbox:latest)'}")
    print(f"sandbox.tier:  {policy.resolve_tier(p, 'isolated')!r} (effective)")

    if p.browser.cookies_env:
        state = "set" if os.environ.get(p.browser.cookies_env, "") else "UNSET"
        print(f"browser.cookies_env: {p.browser.cookies_env} ({state})")

    return 1 if missing else 0


def _sandbox(cmd: str) -> int:
    """Docker sandbox lifecycle, harness-owned (Phase 8g). Replaces the Go
    ``task start/stop/status`` for container boot. ``ensure`` reuses a running
    container or boots one (the path the exec client takes lazily)."""
    from pux_harness.sandbox.container import SandboxContainer, resolve_project_path

    sb = SandboxContainer()
    project = resolve_project_path()
    org = sb.org or "(none)"

    if cmd == "start":
        name = sb.ensure()
        _print_status(name, project, org)
        return 0
    if cmd == "ensure":
        name = sb.ensure()
        _print_status(name, project, org)
        return 0
    if cmd == "stop":
        sb.destroy()
        print(f"stopped + removed container for {project}")
        return 0
    if cmd == "status":
        from pux_harness.sandbox.docker_exec import _discover  # noqa: PLC0415
        import docker  # noqa: PLC0415

        name = _discover(docker.from_env(timeout=10), project)
        if name is None:
            print(f"not running (no container for {project})")
            return 1
        _print_status(name, project, org)
        return 0
    raise SystemExit(
        f"unknown sandbox subcommand {cmd!r}; use: start | stop | status | ensure"
    )


def _print_status(name: str, project: str, org: str) -> None:
    import docker  # noqa: PLC0415

    c = docker.from_env(timeout=10).containers.get(name)
    print(f"running")
    print(f"  Container   {name}")
    print(f"  Image       {c.image.tags[0] if c.image.tags else c.image.id[:19]}")
    print(f"  Status      {c.status}")
    print(f"  Project     {project}")
    print(f"  Org policy  {org}")
    print(f"  Network     {','.join(c.attrs['NetworkSettings']['Networks'].keys())}")
    print(f"  Runtime     {c.attrs['HostConfig']['Runtime'] or 'default'}")


def _check_contract() -> int:
    """Run the declarative org contract — fully offline (no server, no tokens).
    Rule 4 (tool-resolution) resolves against the static native surface (fs
    tools ∪ the specialist registry), so it runs identically in pytest and
    here with nothing live."""
    per_org = check_all()
    for org in sorted(per_org):
        vs = per_org[org]
        print(f"\n## {org}")
        if vs:
            for x in vs:
                print(f"  {x}")
        else:
            print("  OK")

    harness_vs = check_harness()
    print("\n## harness (global)")
    for x in harness_vs:
        print(f"  {x}")
    if not harness_vs:
        print("  OK")

    skill_vs = check_skill_roots()
    print("\n## skills (global)")
    for x in skill_vs:
        print(f"  {x}")
    if not skill_vs:
        print("  OK")

    n_orgs = len(per_org)
    error_orgs = [o for o, vs in per_org.items() if has_errors(vs)]
    harness_errors = has_errors(harness_vs)
    skill_errors = has_errors(skill_vs)
    print(f"\n{n_orgs} orgs checked.")
    if error_orgs:
        print(f"BLOCKING errors in: {error_orgs}")
    if harness_errors:
        print("BLOCKING errors in harness (global).")
    if skill_errors:
        print("BLOCKING errors in skills (global).")
    return 1 if (error_orgs or harness_errors or skill_errors) else 0


def main() -> None:
    ap = argparse.ArgumentParser(description="deepagents Pux harness")
    ap.add_argument("--org", default="general", help="org to run (default: general)")
    ap.add_argument("--task", help="task string (default: per-org forcing task)")
    ap.add_argument("--recursion-limit", type=int, default=60)
    ap.add_argument("--check", action="store_true", help="docker-exec backend + native specialist smoke, no model call")
    ap.add_argument("--list", action="store_true", help="list discovered orgs + their agents")
    ap.add_argument("--check-contract", action="store_true",
                    help="validate the declarative org contract; exit 1 on error")
    ap.add_argument("--check-policy", action="store_true",
                    help="resolve + report this org's policy (mounts/creds/egress/tier); "
                         "exit 1 if required creds are missing. No model call.")
    ap.add_argument("--sandbox", metavar="CMD",
                    help="Docker sandbox lifecycle: start | stop | status | ensure "
                         "(harness-owned, Phase 8g; replaces `task start/stop/status`)")
    ap.add_argument("--jobs-run", action="store_true",
                    help="run prep jobs for this org inside the sandbox (Phase 14)")
    ap.add_argument("--jobs-status", action="store_true",
                    help="show declared prep jobs for this org (Phase 14)")
    ap.add_argument("--job", default=None,
                    help="with --jobs-run: run only this named job")
    args = ap.parse_args()

    if args.sandbox is not None:
        raise SystemExit(_sandbox(args.sandbox))

    if args.jobs_run:
        raise SystemExit(_jobs_run(args.org, args.job))

    if args.jobs_status:
        raise SystemExit(_jobs_status(args.org))

    if args.check_contract:
        raise SystemExit(_check_contract())

    if args.list:
        orgs = discover_orgs()
        print(f"{len(orgs)} orgs:")
        for org in orgs:
            print(f"  {org}: {', '.join(org_agent_slugs(org)) or '(no agents)'}")
        return

    if args.org not in discover_orgs():
        raise SystemExit(f"unknown org {args.org!r}; discovered: {discover_orgs()}")

    if args.check_policy:
        raise SystemExit(_check_policy(args.org))

    if args.check:
        exec_client = get_exec_client()
        backend = PuxSandboxBackend(exec_client)
        specialists = build_native_specialists(exec_client)
        print(f"backend(docker exec) OK: {len(specialists)} native pux_sandbox_* specialists + "
              f"native fs (ls/read_file/write_file/edit_file/glob/grep/execute)")
        ex = backend.execute("echo pux-ok")
        print(f"  backend.execute [docker exec]: exit={ex.exit_code} output={ex.output!r}")
        ls = backend.ls("/sandbox/workspace")
        print(f"  backend.ls: {len(ls.entries or [])} entries, error={ls.error}")
        return

    task = args.task or DEFAULT_TASKS[args.org]
    asyncio.run(_run(args.org, task, args.recursion_limit))


if __name__ == "__main__":
    main()
