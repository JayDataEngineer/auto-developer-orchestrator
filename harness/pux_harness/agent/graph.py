"""Per-org deepagents graph builder, shared by the in-process runner
(``main.py``) and the Agent Protocol server (``server.py``).

One ``DockerExecClient`` + one ``PuxSandboxBackend`` serve the whole process
(the client is a thin Docker SDK wrapper; the backend is stateless apart from
an observation log). Per-org compiled graphs are built lazily and cached by
the caller — building is expensive (model init + subagent assembly) and the
only per-org variation is system_prompt + subagents + the specialist-tool
whitelist. All 13 specialists are native Python tools (Phase 8i deleted the Go
bridge that used to supply them over MCP).
"""

from __future__ import annotations

from typing import Any

from deepagents import create_deep_agent
from langgraph.graph.state import CompiledStateGraph

from pux_harness.agent.model import get_model
from pux_harness.agent.orgs import build_system_prompt, load_subagents
from pux_harness.agent.profile import apply_profile_to_tools, load_profile
from pux_harness.context.offload import ContextOffloadMiddleware, build_ctx_tools
from pux_harness.sandbox.backend import PuxSandboxBackend
from pux_harness.sandbox.docker_exec import DockerExecClient, get_exec_client
from pux_harness.sandbox.tools import build_native_specialists

_exec: DockerExecClient | None = None  # direct docker exec — fs/shell + specialists
_backend: PuxSandboxBackend | None = None


def shared_exec() -> DockerExecClient:
    """One docker-exec client for the process (lazy — discovery hits Docker)."""
    global _exec
    if _exec is None:
        _exec = get_exec_client()
    return _exec


def shared_backend() -> PuxSandboxBackend:
    """One sandbox backend over the shared docker-exec client."""
    global _backend
    if _backend is None:
        _backend = PuxSandboxBackend(shared_exec())
    return _backend


def build_graph(org: str, *, checkpointer: Any) -> CompiledStateGraph:
    """Compile the deepagents graph for ``org`` against ``checkpointer``.

    Specialist ``pux_sandbox_*`` tools come from ``tools=`` (all native);
    native fs/shell tools come from ``FilesystemMiddleware`` via the shared
    backend (auto-injected into the main agent + every subagent by
    ``create_deep_agent``). The checkpointer is caller-supplied so the runner
    can use an ephemeral ``MemorySaver`` while the server uses a persistent
    ``AsyncSqliteSaver``.

    Per-org ``orgs/<org>/profile.yaml`` (Phase 16.3b; OPTIONAL — most orgs ship
    none) applies three org-wide overrides to the CTO stack:
    ``base_system_prompt`` (full prompt replace), ``system_prompt_suffix``
    (appended), ``tool_description_overrides`` + ``excluded_tools`` (applied via
    ``profile.apply_profile_to_tools``). The same profile is threaded into
    ``load_subagents`` so the suffix + tool overrides reach EACH specialist
    subagent (e.g. the shared browser agent) — the user's stated goal. With no
    profile this path is byte-identical to a profile-less build.
    """
    # Roles (Phase 17.B.0): the CTO runs on `base`; describe_image runs on
    # `multimodal` (decoupled so an org can pin a vision model != the driver).
    # Both resolve through models.yaml + org profile + env, never a hardcoded id.
    base_model = get_model(role="base", org=org)
    specialists = build_native_specialists(
        shared_exec(), vision_model=get_model(role="multimodal", org=org), org=org,
    )
    # Phase 7: ctx_recall/ctx_search ride on the MAIN agent only (they're not in
    # any subagent ``tools:`` whitelist, so excluding them from the subagent-
    # resolution ``tools`` keeps specialist whitelists clean). The offload
    # middleware shares the process-wide store with these tools via shared_store().
    # Main-agent-only: deepagents' SubAgentMiddleware doesn't forward a raw
    # spec's `middleware` key (verified in the Phase 7 E2E), so attaching it to
    # specialists is a silent no-op — see context_offload.py module docstring.
    ctx_tools = build_ctx_tools()

    prompt = build_system_prompt(org)
    main_tools: list = [*specialists, *ctx_tools]
    cfg = load_profile(org)
    if cfg is not None:
        if cfg.base_system_prompt:
            prompt = cfg.base_system_prompt
        if cfg.system_prompt_suffix:
            prompt = f"{prompt}\n\n{cfg.system_prompt_suffix}"
        main_tools = apply_profile_to_tools(main_tools, cfg)

    return create_deep_agent(
        model=base_model,
        system_prompt=prompt,
        tools=main_tools,
        subagents=load_subagents(org, specialists, profile=cfg),
        middleware=[ContextOffloadMiddleware()],
        backend=shared_backend(),
        checkpointer=checkpointer,
    )
