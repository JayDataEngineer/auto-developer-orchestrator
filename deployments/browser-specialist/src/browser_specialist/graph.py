"""browser-specialist — the 42-tool stealth browser as an isolated subagent.

Deployed on Aegra (self-hosted LangGraph Platform alternative) and exposed to
dcode through the native `[async_subagents]` seam in ~/.deepagents/config.toml.
Isolation is enforced at THREE boundaries, each in its natural place:

1. dcode <-> specialist (context isolation): the main agent never sees the
   browser tools — dcode loads only the five `*_async_task` middleware tools
   from the config seam; the 42 browser tool schemas never enter any dcode
   session. The specialist likewise has none of the main agent's tools: this
   graph is built from scratch with the browser MCP only.

2. The graph process itself (tool isolation): `create_deep_agent` is
   ADDITIVE — the built-in file/shell/subagent suite (`ls`, `read_file`,
   `write_file`, `edit_file`, `glob`, `grep`, `delete`, `execute`, `task`)
   would ride along on top of the browser tools, handing the agent
   host-filesystem access from inside the trusted Aegra tier (this
   deployment's `.env` holds the model token). The sanctioned seam to drop
   built-ins is a HarnessProfile with `excluded_tools` (registered
   provider-wide — this deployment serves exactly one model family), plus
   `GeneralPurposeSubagentProfile(enabled=False)` to remove `task`. The
   graph's complete toolset is then EXACTLY the 42 browser tools.

3. Browser execution (host isolation): the browser tier is a REAL sandbox,
   not guard-rails inside the tool server. mc_browser and its Chromium run
   inside an OpenSandbox container (`sandbox/Dockerfile`, the platform we
   already run on :8080); this graph talks to it over MCP HTTP. The
   container carries no credentials and cannot reach the host filesystem,
   host processes, or this process's `.env` — escape hatches inside the
   container (the `run` tool, file:// pages) are bounded by the container,
   which is the point. See `browser_specialist.sandbox`.

Skills are structurally isolated as well: `skills=` is never passed here
(no SkillsMiddleware), so no dcode workspace/plugin skill can load into
this process; if the specialist ever needs one, it is declared in THIS
deployment and nowhere else.

The graph is exposed as a *runtime factory* per the langgraph-sdk contract:
Aegra calls `make_graph(runtime)` inside its own event loop, so the toolkit
(and its connection to the browser sandbox) live on a loop that never tears
down. The toolkit is built once on first *execution* and cached in a module
global. Introspection calls (`runtime.execution_runtime is None`) get a
tools-free graph: schema reads never touch the sandbox.
"""

from __future__ import annotations

import re
from pathlib import Path
from deepagents import (
    GeneralPurposeSubagentProfile,
    HarnessProfile,
    create_deep_agent,
    register_harness_profile,
)
from langchain_mcp_adapters.client import MultiServerMCPClient
from langgraph_sdk.runtime import ServerRuntime

from browser_specialist import sandbox as browser_sandbox
from browser_specialist.utils import build_model

REPO_ROOT = Path(__file__).resolve().parents[4]
BROWSER_AGENT_MD = REPO_ROOT / ".deepagents" / "agents" / "browser" / "AGENTS.md"

# The sealed-browser harness policy (see module docstring, boundary 2).
# Registered at import so every create_deep_agent call in this process drops
# the built-ins.
register_harness_profile(
    "openai",
    HarnessProfile(
        excluded_tools=frozenset(
            {
                "ls",
                "read_file",
                "write_file",
                "edit_file",
                "glob",
                "grep",
                "delete",
                "execute",
            }
        ),
        general_purpose_subagent=GeneralPurposeSubagentProfile(enabled=False),
    ),
)


def _system_prompt() -> str:
    """The authored browser agent body, YAML frontmatter stripped."""
    text = BROWSER_AGENT_MD.read_text()
    if text.startswith("---"):
        text = re.sub(r"\A---\n.*?\n---\n?", "", text, count=1, flags=re.S)
    return text.strip()


# One toolkit for the process lifetime. Built on first execution, on the
# server's own loop; connecting is idempotent (the sandbox module reuses the
# persisted sandbox), so the cache also makes reconnects cheap.
_TOOLKIT: MultiServerMCPClient | None = None


async def _execution_tools() -> list:
    """The browser MCP tools, served from inside the OpenSandbox container."""
    global _TOOLKIT
    if _TOOLKIT is None:
        sbx = await browser_sandbox.ensure_browser_sandbox()
        _TOOLKIT = MultiServerMCPClient(
            {"sandbox_browser": await browser_sandbox.mcp_connection(sbx)}
        )
    return await _TOOLKIT.get_tools()


def _is_execution(runtime: ServerRuntime) -> bool:
    """True when this factory call is a real run (not schema introspection).

    Duck-typed: Aegra may hand over a `ServerRuntime` dataclass or its raw
    dict form, depending on the call path.
    """
    ert = getattr(runtime, "execution_runtime", None)
    if ert is not None:
        return True
    if isinstance(runtime, dict):
        return bool(runtime.get("execution_runtime")) or runtime.get("access_context") == "threads.create_run"
    return True  # unknown form — assume execution; the cache makes this cheap


async def make_graph(runtime: ServerRuntime):
    """Runtime factory — Aegra invokes this per run, inside its event loop.

    The parameter annotation MUST resolve at runtime (no TYPE_CHECKING-only
    imports): Aegra classifies the parameter by its resolved annotation —
    `ServerRuntime` gets the runtime object, anything else gets the
    RunnableConfig dict.
    """
    tools = await _execution_tools() if _is_execution(runtime) else []
    return create_deep_agent(
        model=build_model(),
        tools=tools,
        system_prompt=_system_prompt(),
    )
