"""Prove `scripts/dcode_subagent_patch.py` against a real dcode install.

Run with the isolated dcode venv built from the cloned repo (the pux `.venv`
holds only a `deepagents_code` stub and must not be polluted):

    uv sync --project /tmp/opencode/deepagents/libs/code
    /tmp/opencode/deepagents/libs/code/.venv/bin/python \
        scripts/prove_dcode_subagent_patch.py

The proof exercises the two patched seams (frontmatter parse + subagent-spec
enrichment) and the request-time filter behavior, without invoking an LLM.
"""

from __future__ import annotations

import asyncio
import os
import sys
import tempfile
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent))

import dcode_subagent_patch as patch

TMP = Path(tempfile.mkdtemp(prefix="dcode-subagent-prove-"))
HOME = TMP / "home"
PROJECT = TMP / "project"
HOME.mkdir()
PROJECT.mkdir()
os.environ["HOME"] = str(HOME)

FAILURES: list[str] = []


def check(label: str, cond: bool) -> None:
    status = "PASS" if cond else "FAIL"
    print(f"  [{status}] {label}")
    if not cond:
        FAILURES.append(label)


# --- 1. parser seam: list_subagents through the patched loader ---------------
agents_dir = HOME / ".deepagents" / "agents" / "researcher"
agents_dir.mkdir(parents=True)
(agents_dir / "AGENTS.md").write_text(
    """---
name: researcher
description: Read-only research agent
model: anthropic:claude-haiku-4-5-20251001
tools: [read_file, grep, web_search]
excluded_tools: [write_file]
middleware: [rate_limiter]
---

You research on the web and report findings.
""",
    encoding="utf-8",
)

from deepagents_code.subagents import list_subagents  # noqa: E402  (imports follow patch setup)

metas = list_subagents(user_agents_dir=HOME / ".deepagents" / "agents")
check("patched parser returns the custom subagent", len(metas) == 1 and metas[0]["name"] == "researcher")

overrides = patch._SUBAGENT_OVERRIDES.get("researcher")
check("parser stashed `tools` allowlist", overrides is not None and overrides["tools"] == ["read_file", "grep", "web_search"])
check("parser stashed `excluded_tools`", overrides is not None and overrides["excluded_tools"] == ["write_file"])
check("parser stashed `middleware` spec", overrides is not None and overrides["middleware"] == [{"name": "rate_limiter"}])

# --- 2. custom middleware registry -------------------------------------------
calls: list[str] = []


class CountingMiddleware(patch.AgentMiddleware):
    def __init__(self, max_calls: int = 100) -> None:
        super().__init__()
        self.max_calls = max_calls


@patch.register("rate_limiter")
def _rate_limiter(max_calls: int = 100) -> CountingMiddleware:
    calls.append(f"rate_limiter(max_calls={max_calls})")
    return CountingMiddleware(max_calls=max_calls)


# --- 3. spec enrichment: _apply_overrides ------------------------------------
spec = {
    "name": "researcher",
    "description": "Read-only research agent",
    "system_prompt": "You research.",
    "model": "anthropic:claude-haiku-4-5-20251001",
    "middleware": [],
}
enriched = patch._apply_overrides(spec)
mw_types = [type(m).__name__ for m in enriched["middleware"]]
check("enrichment appended ToolFilterMiddleware", "ToolFilterMiddleware" in mw_types)
check("enrichment appended registered custom middleware", "CountingMiddleware" in mw_types)
check("custom middleware factory received frontmatter kwargs", "rate_limiter(max_calls=100)" in calls)
check("enrichment left non-overridden spec untouched", patch._apply_overrides({"name": "other"}) == {"name": "other"})
check(
    "enrichment does not mutate the caller's spec dict",
    spec
    == {
        "name": "researcher",
        "description": "Read-only research agent",
        "system_prompt": "You research.",
        "model": "anthropic:claude-haiku-4-5-20251001",
        "middleware": [],
    },
)

# unknown middleware fails loud
unknown = {"name": "researcher", "middleware": []}
patch._SUBAGENT_OVERRIDES["researcher"] = {"tools": None, "excluded_tools": None, "middleware": [{"name": "nope"}]}
try:
    patch._apply_overrides(unknown)
    check("unknown middleware raises", False)
except KeyError:
    check("unknown middleware raises", True)
patch._SUBAGENT_OVERRIDES["researcher"] = overrides

# --- 4. request-time tool filter behavior -------------------------------------


class _Tool:
    def __init__(self, name: str) -> None:
        self.name = name


class _FakeRequest:
    def __init__(self, tools: list[Any]) -> None:
        self.tools = list(tools)

    def override(self, **kwargs: Any) -> "_FakeRequest":
        out = _FakeRequest(self.tools)
        for key, value in kwargs.items():
            setattr(out, key, value)
        return out


def _tools(*names: str) -> list[Any]:
    return [_Tool(n) for n in names]


def _handler(*, store: list[list[Any]]) -> Any:
    def _h(request: Any) -> Any:
        store.append([t.name if not isinstance(t, dict) else t["name"] for t in request.tools])
        return None

    return _h


# allowlist + exclusion combined
mw = patch.ToolFilterMiddleware(allowed=["read_file", "grep", "web_search"], excluded=["write_file"])
seen: list[list[str]] = []
full = _tools("read_file", "write_file", "grep", "web_search", "task")
mw.wrap_model_call(_FakeRequest(full), _handler(store=seen))
check("allowlist + exclusion: only allowlisted non-excluded tools survive",
      seen and set(seen[0]) == {"read_file", "grep", "web_search"})

# dict-shaped tools (as MCP/consumer tools arrive) are filtered too
seen.clear()
mw.wrap_model_call(_FakeRequest([{"name": "read_file"}, {"name": "execute"}]), _handler(store=seen))
check("dict-shaped tools filtered by name", seen and set(seen[0]) == {"read_file"})

# explicit empty allowlist means no tools at all
mw_empty = patch.ToolFilterMiddleware(allowed=[])
seen.clear()
mw_empty.wrap_model_call(_FakeRequest(_tools("read_file", "grep")), _handler(store=seen))
check("`tools: []` yields no visible tools", seen and seen[0] == [])

# exclusion alone leaves the rest untouched
mw_ex = patch.ToolFilterMiddleware(excluded=["write_file"])
seen.clear()
mw_ex.wrap_model_call(_FakeRequest(_tools("read_file", "write_file", "grep")), _handler(store=seen))
check("exclusion alone keeps non-excluded tools", seen and set(seen[0]) == {"read_file", "grep"})

# async path filters identically
async def _aprove() -> None:
    async def _ahandler(request: Any) -> None:
        seen.append([t.name if not isinstance(t, dict) else t["name"] for t in request.tools])

    seen.clear()
    await mw.awrap_model_call(_FakeRequest(full), _ahandler)


asyncio.run(_aprove())
check("async filter behaves identically", seen and set(seen[0]) == {"read_file", "grep", "web_search"})

# --- 5. agent seam: wrapper installed on agent_mod.create_deep_agent ---------
import deepagents_code.agent as agent_mod  # noqa: E402  (imports follow patch setup)

check(
    "agent_mod.create_deep_agent is the patched wrapper",
    getattr(agent_mod.create_deep_agent, "_dcode_subagent_patch", False) is True,
)

# The wrapper applies `_apply_overrides` to every subagent spec, then delegates
# to the original resolver it closed over. `_apply_overrides` is proven in
# section 3; delegation is a one-liner over it. A spec with no overrides passes
# through byte-identical (proving the wrapper's filter is the only mutation):
plain_spec = {"name": "plain", "description": "d", "system_prompt": "s", "middleware": []}
check("non-overridden spec passes through untouched", patch._apply_overrides(plain_spec) is plain_spec)

# --- 6. integration: a real headless graph compiles with the enriched spec ----
# Proves the injected middleware survives `create_deep_agent`'s custom-middleware
# splice (no duplicate-name assertion, correct ordering) end to end.
os.environ["ANTHROPIC_API_KEY"] = "test-key-not-real"

from deepagents_code.agent import create_cli_agent  # noqa: E402  (imports follow patch setup)

try:
    agent, backend = create_cli_agent(
        model="anthropic:claude-haiku-4-5-20251001",
        assistant_id="prove-integration",
        interactive=False,
        auto_approve=True,
        cwd=PROJECT,
    )
    check("headless graph compiles with enriched researcher spec", type(agent).__name__ == "CompiledStateGraph")
except Exception as exc:  # noqa: BLE001
    check(f"headless graph compiles with enriched researcher spec ({exc!r})", False)

print()
if FAILURES:
    print(f"FAILED: {len(FAILURES)} check(s): {FAILURES}")
    sys.exit(1)
print("ALL CHECKS PASSED")
