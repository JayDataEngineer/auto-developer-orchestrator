"""Phase 3 harness: run any ported org on deepagents over the Go-MCP bridge,
with a NATIVE fs/shell surface backed by ``PuxSandboxBackend``.

  uv run python -m pux_harness.main --list                       # ported orgs + agents
  uv run python -m pux_harness.main --check                      # bridge + backend smoke (no tokens)
  uv run python -m pux_harness.main --org general                # default forcing task
  uv run python -m pux_harness.main --org dev-bot --task "..."   # custom task

Proves per-org: deepagents drives the pux sandbox through ``PuxSandboxBackend``
(native ``ls/read_file/write_file/edit_file/glob/grep/execute``), the CTO
delegates to its specialist via ``task(subagent_type=...)``, and specialists
fall back to those same native tools. Specialist capabilities (browser/desktop/
vision/skills/python) remain ``pux_sandbox_*`` MCP tools. Prints a message
trace + token usage so cost/output is comparable.
"""
from __future__ import annotations

import argparse
import asyncio
import uuid

from langgraph.checkpoint.memory import MemorySaver

from pux_harness.bridge import get_pux_client, get_pux_tools
from pux_harness.contract import check_all, check_harness, has_errors
from pux_harness.graph import build_graph, shared_backend
from pux_harness.orgs import (
    discover_orgs,
    org_agent_slugs,
)
from pux_harness.sandbox import PuxSandboxBackend

# Per-org default forcing tasks. Each is UN-answerable from the system prompt
# alone, so the agent must delegate to its specialist — the seam we're proving.
# Tasks name the NATIVE `execute` tool (Phase 3); `pux_sandbox_bash` is gone.
DEFAULT_TASKS: dict[str, str] = {
    "general": (
        "How many non-test Go source files are under backend/internal/mcpserver/, "
        "and what are their names? Delegate to the `researcher` subagent — do NOT "
        "inspect the codebase yourself. Have it run, via the native `execute` tool: "
        "`find /sandbox/workspace/backend/internal/mcpserver -name '*.go' -not -name "
        "'*_test.go'`. Report the researcher's findings verbatim."
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


def _check_contract() -> int:
    """Run the declarative org contract. Structural tier always runs (no
    server, no tokens); the tool-resolution tier (rule 4) runs only when the
    Go MCP bridge is reachable — announced explicitly, not a silent skip."""
    bridge_tools: set[str] | None = None
    try:
        tools = get_pux_tools()
        bridge_tools = {t.name for t in tools}
        print(f"bridge: live ({len(bridge_tools)} pux_sandbox_* specialists) "
              f"-> tool-resolution ON")
    except Exception as e:  # noqa: BLE001 - announce + degrade one tier, loudly
        print(f"bridge: not reachable ({type(e).__name__}: {e}); "
              f"tool-resolution skipped (structural only)")

    per_org = check_all(bridge_tools=bridge_tools)
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

    n_orgs = len(per_org)
    error_orgs = [o for o, vs in per_org.items() if has_errors(vs)]
    harness_errors = has_errors(harness_vs)
    print(f"\n{n_orgs} orgs checked.")
    if error_orgs:
        print(f"BLOCKING errors in: {error_orgs}")
    if harness_errors:
        print("BLOCKING errors in harness (global).")
    return 1 if (error_orgs or harness_errors) else 0


def main() -> None:
    ap = argparse.ArgumentParser(description="deepagents Pux harness (Phase 3)")
    ap.add_argument("--org", default="general", help="org to run (default: general)")
    ap.add_argument("--task", help="task string (default: per-org forcing task)")
    ap.add_argument("--recursion-limit", type=int, default=60)
    ap.add_argument("--check", action="store_true", help="bridge + backend smoke, no model call")
    ap.add_argument("--list", action="store_true", help="list discovered orgs + their agents")
    ap.add_argument("--check-contract", action="store_true",
                    help="validate the declarative org contract; exit 1 on error")
    args = ap.parse_args()

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

    if args.check:
        client = get_pux_client()
        backend = PuxSandboxBackend(client)
        tools = get_pux_tools(client=client)
        print(f"bridge+backend OK: {len(tools)} specialist pux_sandbox_* tools + "
              f"native fs (ls/read_file/write_file/edit_file/glob/grep/execute)")
        ex = backend.execute("echo pux-ok")
        print(f"  backend.execute: exit={ex.exit_code} output={ex.output!r}")
        ls = backend.ls("/sandbox/workspace")
        print(f"  backend.ls: {len(ls.entries or [])} entries, error={ls.error}")
        return

    task = args.task or DEFAULT_TASKS[args.org]
    asyncio.run(_run(args.org, task, args.recursion_limit))


if __name__ == "__main__":
    main()
