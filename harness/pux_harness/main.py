"""Phase 1 harness: run any ported org on deepagents over the Go-MCP bridge.

  uv run python -m pux_harness.main --list                       # ported orgs + agents
  uv run python -m pux_harness.main --check                      # bridge + profile smoke (no tokens)
  uv run python -m pux_harness.main --org general                # default forcing task
  uv run python -m pux_harness.main --org dev-bot --task "..."   # custom task

Proves per-org: deepagents drives the pux sandbox via the bridge, the CTO
delegates to the right specialist via `task(subagent_type=...)`, and the
only file/shell surface is `pux_sandbox_*` (native fs tools excluded).
Prints a message trace + token usage so cost/output is comparable.
"""
from __future__ import annotations

import argparse
import asyncio
import uuid

from langgraph.checkpoint.memory import MemorySaver

from deepagents import create_deep_agent

from pux_harness.bridge import PUX_PREFIX, get_pux_tools
from pux_harness.contract import check_all, check_harness, has_errors
from pux_harness.model import _NATIVE_FS_TOOLS, get_model, register_pux_profile
from pux_harness.orgs import (
    build_system_prompt,
    discover_orgs,
    load_subagents,
    org_agent_slugs,
)

# Per-org default forcing tasks. Each is UN-answerable from the system prompt
# alone, so the agent must delegate to its specialist — the seam we're proving.
DEFAULT_TASKS: dict[str, str] = {
    "general": (
        "How many non-test Go source files are under backend/internal/mcpserver/, "
        "and what are their names? Delegate to the `researcher` subagent — do NOT "
        "inspect the codebase yourself. Have it run, via pux_sandbox_bash: "
        "`find /sandbox/workspace/backend/internal/mcpserver -name '*.go' -not -name "
        "'*_test.go'`. Report the researcher's findings verbatim."
    ),
    "_demo": (
        "List the top-level entries of the project root inside the sandbox. Delegate "
        "to the `researcher` subagent — do NOT run tools yourself. Have it use "
        "pux_sandbox_bash: `ls -1 /sandbox/workspace`. Report its findings verbatim."
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
}


def _build_agent(org: str):
    register_pux_profile()
    model = get_model()
    all_tools = get_pux_tools()
    agent = create_deep_agent(
        model=model,
        system_prompt=build_system_prompt(org),
        tools=all_tools,
        subagents=load_subagents(org, all_tools),
        checkpointer=MemorySaver(),
    )
    return agent


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
    agent = _build_agent(org)
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


def _check_contract() -> int:
    """Run the declarative org contract. Structural tier always runs (no
    server, no tokens); the tool-resolution tier (rule 4) runs only when the
    Go MCP bridge is reachable — announced explicitly, not a silent skip."""
    bridge_tools: set[str] | None = None
    try:
        tools = get_pux_tools()
        bridge_tools = {t.name for t in tools}
        print(f"bridge: live ({len(bridge_tools)} pux_sandbox_* tools) "
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
    ap = argparse.ArgumentParser(description="deepagents Pux harness (Phase 1)")
    ap.add_argument("--org", default="general", help="org to run (default: general)")
    ap.add_argument("--task", help="task string (default: per-org forcing task)")
    ap.add_argument("--recursion-limit", type=int, default=60)
    ap.add_argument("--check", action="store_true", help="bridge + profile smoke, no model call")
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

    register_pux_profile()
    if args.check:
        tools = get_pux_tools()
        print(f"bridge: {len(tools)} pux_sandbox_* tools")
        print(f"profile 'openai' excludes native fs: {sorted(_NATIVE_FS_TOOLS)}")
        bash = next((t for t in tools if t.name == PUX_PREFIX + "bash"), None)
        if bash:
            print("--- bash smoke: ls /sandbox/workspace ---")
            print(bash.invoke({"command": "ls /sandbox/workspace 2>/dev/null | head -20"}))
        return

    task = args.task or DEFAULT_TASKS[args.org]
    asyncio.run(_run(args.org, task, args.recursion_limit))


if __name__ == "__main__":
    main()
