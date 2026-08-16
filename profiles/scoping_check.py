#!/usr/bin/env python3
"""Scoping drift check — prove the model-keyed MCP bridge holds, or fail loudly.

The bridge (plugins/tool-scoping, docs/isolation-patterns.md "L2.5") scopes MCP
tools away from no-MCP subagents via a harness profile keyed on the subagent's
own model (frontmatter `model: openai:glm-5-turbo`). It is a DENY list, so it
fails OPEN: a server that adds a tool after capture, an edited allowedTools
trim, or a silently-skipped plugin registration would all leak tools into
scoped agents. This check rebuilds every profile through dcode's own assembly
(create_cli_agent) and asserts:

  1. the profile registered (entry-point plugin loaded in this venv) and its
     exclusion set covers every live MCP tool name in the workspace;
  2. each profile's scoped roster is exactly the authored set (drift in either
     direction — a dropped or an accidental `model:` — fails);
  3. every scoped agent carries _ToolExclusionMiddleware and its EFFECTIVE MCP
     toolset (spec tools − excluded) is empty — leaked names are printed;
  4. no unscoped agent carries any exclusion middleware (that would mean a
     profile key collided with a real session model);
  5. every ${VAR} placeholder in every .mcp.json resolves in the environment
     (an unset URL silently serves zero tools — that is how nitter sat at
     0 tools for days behind a healthy container: hard FAIL, not a warn);
  6. declared-but-empty servers are WARNed (down servers masquerade as
     "scoped clean"; the check names them so it can't be mistaken for passing)
     — except profiles in EXPECTED_EMPTY_MCP, where zero servers is the design.

Run: make scoping-check   (or: $(dcode python) profiles/scoping_check.py)
"""
from __future__ import annotations

import asyncio
import os
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCOPED_MODEL = "openai:glm-5-turbo"
PROFILES = ["coding", "research", "invest", "game", "media", "social"]
# The authored scoped set (see docs/isolation-patterns.md). Agents are shared
# across profiles via the symlink union, so a scoped agent must need zero MCP
# EVERYWHERE it is rostered — web-search is deliberately unscoped (research
# needs its web_research tools).
EXPECTED_SCOPED = {
    "coding": {"task-planner", "web-agent"},
    "game": {"game-studio-docs-writer", "task-planner"},
    "research": set(), "invest": set(), "media": set(), "social": set(),
}
# Profiles that declare ZERO MCP servers on purpose (coding: git + github go
# through `execute` + the gh CLI — 63 tool schemas were riding every request
# for work the shell already does). Their "served 0 tools" is the design, not
# an outage; any OTHER empty server still WARNs.
EXPECTED_EMPTY_MCP = {"coding"}


def _load_env() -> None:
    for line in (REPO / ".env").read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, _, v = line.partition("=")
            os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))
    # The check never invokes a model; construction just needs a key present.
    os.environ.setdefault("OPENAI_API_KEY", "scoping-check-not-invoked")


def _check_placeholders(failures: list[str]) -> None:
    """Every ${VAR} in every .mcp.json must resolve — unset = silent 0 tools."""
    import re

    env_keys = {k for k, v in os.environ.items() if v}
    mcp_files = [REPO / ".mcp.json", *sorted((REPO / "profiles").glob("*/.mcp.json"))]
    for f in mcp_files:
        for var in sorted(set(re.findall(r"\$\{(\w+)\}", f.read_text()))):
            if var not in env_keys:
                failures.append(
                    f"{f.relative_to(REPO)}: placeholder ${{{var}}} is NOT set in the "
                    f"environment — the server silently serves 0 tools (this is how "
                    f"nitter sat dark behind a healthy container)")


async def main() -> int:
    _load_env()

    from deepagents.profiles._builtin_profiles import _ensure_builtin_profiles_loaded
    from deepagents.profiles.harness.harness_profiles import _HARNESS_PROFILES

    import deepagents.middleware.subagents as SA
    from deepagents.middleware._tool_exclusion import _ToolExclusionMiddleware
    from langchain_openai import ChatOpenAI

    from deepagents_code.agent import create_cli_agent
    from deepagents_code.mcp_tools import resolve_and_load_mcp_tools
    from deepagents_code.project_utils import ProjectContext
    from deepagents_code.subagents import list_subagents

    failures: list[str] = []
    warnings: list[str] = []

    # ── 0. every MCP URL placeholder resolves ──────────────────────────────
    _check_placeholders(failures)

    # ── 1. the profile registered and is complete ──────────────────────────
    _ensure_builtin_profiles_loaded()
    profile = _HARNESS_PROFILES.get(SCOPED_MODEL)
    if profile is None:
        failures.append(
            f"harness profile {SCOPED_MODEL!r} is NOT registered — the "
            "tool-scoping plugin failed to load (install it: uv pip install "
            "--python \"$(uv tool dir)/deepagents-code/bin/python\" "
            "./plugins/tool-scoping)")
        excluded: frozenset[str] = frozenset()
    else:
        excluded = profile.excluded_tools
        if not excluded:
            failures.append(f"harness profile {SCOPED_MODEL!r} registered with an EMPTY exclusion set")

    captured: list[dict] = []
    _orig = SA.create_sub_agent

    def _capture(spec, **kw):
        captured.append(spec)
        return _orig(spec, **kw)

    SA.create_sub_agent = _capture

    all_live_names: set[str] = set()
    try:
        for prof in PROFILES:
            root = REPO / "profiles" / prof
            ctx = ProjectContext(user_cwd=REPO, project_root=root)
            mcp_tools, sm, infos = await resolve_and_load_mcp_tools(
                project_context=ctx, no_mcp=False, trust_project_mcp=True)
            owner: dict[str, str] = {}
            live: set[str] = set()
            for info in infos:
                for t in info.tools:
                    owner[getattr(t, "name", str(t))] = info.name
                    live.add(getattr(t, "name", str(t)))
                if not info.tools and prof not in EXPECTED_EMPTY_MCP:
                    warnings.append(f"{prof}: server '{info.name}' served 0 tools — down or "
                                    f"misconfigured. The deny-list covers its captured/declared "
                                    f"names; run plugins/tool-scoping/regenerate.py when it "
                                    f"returns to verify against the live surface")
            all_live_names |= live

            roster = {s["name"]: s.get("model") for s in
                      list_subagents(project_agents_dir=ctx.project_agents_dir())}
            scoped = {n for n, m in roster.items() if m == SCOPED_MODEL}

            # ── 2. the authored scoped set is exactly what's on disk ────────
            want = EXPECTED_SCOPED[prof]
            if scoped != want:
                failures.append(f"{prof}: scoped roster drift — expected {sorted(want)}, "
                                f"frontmatter says {sorted(scoped)}")

            # ── 3/4. rebuild through dcode's own assembly ───────────────────
            captured.clear()
            create_cli_agent(
                model=ChatOpenAI(model="scoping-check", api_key="not-invoked"),
                assistant_id=f"scoping-check-{prof}", tools=mcp_tools,
                mcp_tools=mcp_tools, mcp_server_info=infos,
                project_context=ctx, cwd=REPO)
            seen: dict[str, dict] = {}
            for spec in captured:
                seen.setdefault(spec["name"], spec)

            for name, spec in sorted(seen.items()):
                excl = [m for m in spec.get("middleware", [])
                        if isinstance(m, _ToolExclusionMiddleware)]
                spec_mcp = [getattr(t, "name", str(t)) for t in spec.get("tools") or []
                            if getattr(t, "name", str(t)) in live]
                if name in scoped:
                    if not excl:
                        failures.append(f"{prof}/{name}: scoped but carries NO exclusion "
                                        f"middleware ({len(spec_mcp)} MCP tools reachable)")
                        continue
                    eff = sorted(set(spec_mcp) - excluded)
                    if eff:
                        failures.append(f"{prof}/{name}: MCP tools LEAKED past the deny-list: {eff}")
                elif excl:
                    failures.append(f"{prof}/{name}: UNscoped agent carries exclusion middleware "
                                    f"(profile key collision: {[sorted(m._excluded) for m in excl]})")
            try:
                await sm.aclose()
            except Exception:
                pass
    finally:
        SA.create_sub_agent = _orig

    # ── 1b. the deny-list covers everything live right now ─────────────────
    uncovered = sorted(all_live_names - excluded)
    if uncovered:
        failures.append(f"live MCP tool names missing from the deny-list "
                        f"(plugins/tool-scoping YAML is stale): {uncovered}")

    print("=" * 72)
    print(f"scoped tier    : {SCOPED_MODEL}  ({len(excluded)} tools denied)")
    print(f"live MCP names : {len(all_live_names)} across {len(PROFILES)} profiles")
    for w in warnings:
        print(f"  WARN  {w}")
    if failures:
        print("=" * 72)
        for f in failures:
            print(f"  FAIL  {f}")
        print("=" * 72)
        print(f"scoping-check: {len(failures)} FAILURE(S)")
        return 1
    print("scoping-check: PASS — scoped agents hold zero MCP tools, "
          "unscoped agents untouched, deny-list covers every live name")
    return 0


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
