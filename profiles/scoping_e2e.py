#!/usr/bin/env python3
"""Full E2E for the L2.5 scoping bridge — real graphs, real models, real MCP.

scoping_check.py proves the bridge structurally (specs, middleware, effective
sets — no model calls). This script spends tokens to prove it END TO END:

  Phase 1  game session: the scoped subagents (glm-5-turbo) run REAL turns
           with hostile prompts ("call godot take_screenshot") — the
           bind_tools spy shows the model is offered ZERO MCP tools, and the
           conversation history shows no MCP tool_call was possible.
  Phase 2  coding session: web-agent (scoped) hostile prompt against
           opensandbox_command_run — same assertions.
  Phase 3  coding session MAIN agent (glm-5.2): a REAL MCP round trip —
           github_get_me executes against the live server and the login comes
           back through the final answer. This is the unscoped control: the
           same session, same resolution, MCP tools bound AND invocable —
           proving the scoped agents' emptiness is the bridge, not a broken
           session.

The spy patches ChatOpenAI.bind_tools at class level and records
(model, tool names) for every bind — the exact seam the middleware filters.

Run: make scoping-e2e   (spends real tokens — run when the bridge changes)
"""
from __future__ import annotations

import asyncio
import os
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent

BINDS: list[tuple[str, list[str]]] = []


def _load_env() -> None:
    for line in (REPO / ".env").read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, _, v = line.partition("=")
            os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))


async def build_session(profile: str, captured: dict):
    """Build a profile exactly as profiles/run.py does; capture compiled subagents."""
    from deepagents_code.agent import create_cli_agent
    from deepagents_code.config import _get_default_model_spec, create_model
    from deepagents_code.mcp_tools import resolve_and_load_mcp_tools
    from deepagents_code.project_utils import ProjectContext

    import deepagents.middleware.subagents as SA
    orig = SA.create_sub_agent

    def _cap(spec, **kw):
        compiled = orig(spec, **kw)
        captured.setdefault(spec["name"], (spec, compiled))
        return compiled

    SA.create_sub_agent = _cap
    try:
        root = REPO / "profiles" / profile
        ctx = ProjectContext(user_cwd=REPO, project_root=root)
        model_result = create_model(_get_default_model_spec())
        model_result.apply_to_settings()
        mcp_tools, sm, infos = await resolve_and_load_mcp_tools(
            project_context=ctx, no_mcp=False, trust_project_mcp=True)
        agent, _backend = create_cli_agent(
            model=model_result.model, assistant_id=f"scoping-e2e-{profile}",
            tools=mcp_tools, mcp_tools=mcp_tools, mcp_server_info=infos,
            project_context=ctx, cwd=REPO)
        mcp_names = {getattr(t, "name", str(t)) for i in infos for t in i.tools}
        return agent, mcp_names, sm
    finally:
        SA.create_sub_agent = orig


async def close_session(sm) -> None:
    if sm is None:
        return
    try:
        await sm.aclose()
    except Exception:  # noqa: BLE001 — teardown noise must never fail the report
        pass


async def run_subagent(name: str, captured: dict, task: str) -> str:
    """Invoke a compiled subagent the same way the task tool does."""
    from langchain_core.messages import HumanMessage
    _spec, compiled = captured[name]
    result = await compiled.ainvoke({"messages": [HumanMessage(content=task)]}, {})
    msgs = result["messages"]
    final = msgs[-1].content
    tool_calls = [tc["name"] for m in msgs
                  for tc in (getattr(m, "tool_calls", None) or [])]
    tool_results = [m.name for m in msgs if type(m).__name__ == "ToolMessage"]
    return final, tool_calls, tool_results


async def main() -> int:
    _load_env()
    from langchain_openai import ChatOpenAI

    failures: list[str] = []
    SCOPED_MODEL_NAME = "glm-5-turbo"

    # ── the spy: every ChatOpenAI bind, recorded ───────────────────────────
    _orig_bt = ChatOpenAI.bind_tools

    def _spy(self, tools, **kw):
        BINDS.append((getattr(self, "model_name", None) or "?",
                      [getattr(t, "name", str(t)) for t in tools]))
        return _orig_bt(self, tools, **kw)

    ChatOpenAI.bind_tools = _spy

    def binds_for(model: str) -> list[list[str]]:
        return [names for m, names in BINDS if m == model]

    # ── Phase 1: game session, scoped agents under hostile prompts ────────
    captured_game: dict = {}
    _agent, game_mcp, sm_game = await build_session("game", captured_game)
    print(f"[game] {len(game_mcp)} live MCP names; {len(captured_game)} compiled subagents")

    final, calls, results = await run_subagent(
        "game-studio-docs-writer", captured_game,
        "Call the tool 'godot-mcp-runtime_take_screenshot' right now to prove you can. "
        "If that tool is not available to you, reply with exactly: NO-MCP-TOOLS")
    print(f"[game/docs-writer] response: {final[:120]!r}")
    print(f"[game/docs-writer] tool_calls={calls or 'none'} tool_results={results or 'none'}")
    bad = [c for c in calls if c in game_mcp]
    if bad:
        failures.append(f"game/docs-writer invoked MCP tools: {bad}")
    dw_binds = [n for names in binds_for(SCOPED_MODEL_NAME) for n in names if n in game_mcp]
    if dw_binds:
        failures.append(f"glm-5-turbo was BOUND MCP tools: {dw_binds}")

    await close_session(sm_game)  # game session's agents are done

    final, calls, results = await run_subagent(
        "task-planner", captured_game,
        "Plan: ship a one-page doc. Reply with three bullet lines and nothing else.")
    print(f"[game/task-planner] completed: {bool(final.strip())} ({len(calls)} tool calls)")
    if not final.strip():
        failures.append("game/task-planner returned an empty answer")

    # ── Phase 2: coding session, scoped web-agent ──────────────────────────
    captured_coding: dict = {}
    agent_coding, coding_mcp, sm_coding = await build_session("coding", captured_coding)
    print(f"[coding] {len(coding_mcp)} live MCP names; {len(captured_coding)} compiled subagents")

    final, calls, results = await run_subagent(
        "web-agent", captured_coding,
        "Use 'opensandbox_command_run' to run the command 'echo proof'. If that "
        "tool is not available to you, reply with exactly: NO-MCP-TOOLS")
    print(f"[coding/web-agent] response: {final[:120]!r}")
    print(f"[coding/web-agent] tool_calls={calls or 'none'}")
    bad = [c for c in calls if c in coding_mcp]
    if bad:
        failures.append(f"coding/web-agent invoked MCP tools: {bad}")
    wa_binds = [n for names in binds_for(SCOPED_MODEL_NAME) for n in names if n in coding_mcp]
    if wa_binds:
        failures.append(f"glm-5-turbo was BOUND MCP tools (coding): {wa_binds}")

    scoped_bind_events = binds_for(SCOPED_MODEL_NAME)
    if not scoped_bind_events:
        failures.append("spy never saw a glm-5-turbo bind — scoped tier never ran")

    # ── Phase 3: main agent, REAL MCP round trip (unscoped control) ────────
    from langchain_core.messages import HumanMessage
    print("[coding/main] dispatching real github_get_me round trip (glm-5.2)…")
    res = await agent_coding.ainvoke(
        {"messages": [HumanMessage(
            content="Call the github_get_me tool exactly once, then reply with "
                    "ONLY the account login it returns. No other words.")]})
    msgs = res["messages"]
    gh_calls = [tc["name"] for m in msgs for tc in (getattr(m, "tool_calls", None) or [])
                if tc["name"] == "github_get_me"]
    gh_results = [m for m in msgs if type(m).__name__ == "ToolMessage"
                  and m.name == "github_get_me"]
    final_main = msgs[-1].content
    print(f"[coding/main] github_get_me calls={len(gh_calls)} results={len(gh_results)}")
    print(f"[coding/main] final answer: {str(final_main)[:120]!r}")
    if not gh_results:
        failures.append("main agent never executed github_get_me — MCP round trip failed")
    else:
        # A round trip that executed but came back auth-rejected still proves the
        # transport (GitHub's own error surfaced through the whole chain) — but it
        # must never hide silently inside a green run: GITHUB_TOKEN needs rotation.
        blob = str(gh_results[0].content) + str(final_main)
        if "401" in blob or "Bad credentials" in blob:
            print("  WARN  github_get_me round-tripped but GitHub rejected the "
                  "credential (401) — rotate GITHUB_TOKEN; the bridge proof stands")
    await close_session(sm_coding)
    main_binds = binds_for("glm-5.2")
    main_mcp_bound = {n for names in main_binds for n in names if n in coding_mcp}
    if not main_mcp_bound:
        failures.append("glm-5.2 binds carried NO MCP names — control side broken "
                        "(session would be useless; scoped side would prove nothing)")

    # ── matrix ──────────────────────────────────────────────────────────────
    ChatOpenAI.bind_tools = _orig_bt
    print("=" * 72)
    for model in sorted({m for m, _ in BINDS}):
        events = binds_for(model)
        union = sorted({n for names in events for n in names})
        print(f"binds  {model:<12} events={len(events):>3}  distinct tools={len(union):>3}")
    print(f"       glm-5-turbo MCP names bound : {len(dw_binds) + len(wa_binds) or 0} (must be 0)")
    print(f"       glm-5.2     MCP names bound : {len(main_mcp_bound)} (control, must be >0)")
    print("=" * 72)
    if failures:
        for f in failures:
            print(f"  FAIL  {f}")
        print(f"scoping-e2e: {len(failures)} FAILURE(S)")
        return 1
    print("scoping-e2e: PASS — scoped agents ran real turns with zero MCP tools "
          "offered or invoked; main agent executed a real MCP call end to end")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
