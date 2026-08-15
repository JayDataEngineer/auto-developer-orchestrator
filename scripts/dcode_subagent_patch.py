"""Monkey patch: per-subagent tool control + custom middleware for dcode.

dcode's filesystem subagents (``~/.deepagents/agents/{name}/AGENTS.md`` or
``.deepagents/agents/{name}/AGENTS.md``) natively support only ``name``,
``description`` and ``model`` in frontmatter. Every subagent therefore sees the
exact same tool set as the main agent, and there is no way to attach bespoke
middleware per subagent.

This module monkey-patches the two relevant seams in ``deepagents_code``:

* ``deepagents_code.subagents._parse_subagent_file`` -- reads three extra
  frontmatter fields and stashes them in a registry keyed by subagent name:

  - ``tools: [read_file, grep]``     allowlist -- only these tools are visible
                                     to the subagent at request time (empty
                                     list ``[]`` means no tools at all)
  - ``excluded_tools: [write_file]`` deny-list by name
  - ``middleware: [my_middleware]``  instantiate from ``CUSTOM_MIDDLEWARE``

* ``deepagents_code.agent.create_deep_agent`` -- after the CLI assembles the
  final subagent specs, appends a ``ToolFilterMiddleware`` (for ``tools`` /
  ``excluded_tools``) and any registry-built middleware to each matching
  spec's ``middleware`` list before the graph is compiled.

``tools`` is a strict allowlist: the subagent sees only the named tools, so
delegation tools (``task``) and anything else not listed disappear too. To
grant a subagent tools the main agent does not have (additive tools), a
``CUSTOM_MIDDLEWARE`` factory or an explicit tool-factory registry would be
needed -- out of scope for this patch.

Register custom middleware factories and apply:

    import dcode_subagent_patch as patch

    @patch.register("rate_limiter")
    def _rate_limiter(max_calls: int = 10) -> AgentMiddleware:
        return RateLimiterMiddleware(max_calls=max_calls)

    patch.apply()

Frontmatter for the subagent:

    ---
    name: researcher
    description: Read-only research agent
    tools: [read_file, grep, web_search]
    excluded_tools: [write_file]
    middleware: [rate_limiter]
    ---

Applying the patch twice is a no-op.
"""

from __future__ import annotations

import logging
import re
from pathlib import Path
from typing import TYPE_CHECKING, Any, Callable

import yaml
from langchain.agents.middleware.types import AgentMiddleware, ModelRequest, ModelResponse

if TYPE_CHECKING:
    from typing import Awaitable as _Awaitable

    from langchain.agents.middleware.types import (
        ExtendedModelResponse,
        ResponseT,
    )
    from langchain_core.messages import AIMessage

logger = logging.getLogger(__name__)

_SUBAGENT_OVERRIDES: dict[str, dict[str, Any]] = {}
"""Frontmatter overrides by subagent name, filled by the patched parser."""

CUSTOM_MIDDLEWARE: dict[str, Callable[..., Any]] = {}
"""Name -> factory for `middleware:` frontmatter entries. Callable returns an
`AgentMiddleware` instance. Extra frontmatter keys become kwargs."""

_applied = False


def register(name: str) -> Callable[[Callable[..., Any]], Callable[..., Any]]:
    """Decorator registering a custom middleware factory under `name`."""

    def _decorator(factory: Callable[..., Any]) -> Callable[..., Any]:
        CUSTOM_MIDDLEWARE[name] = factory
        return factory

    return _decorator


def _tool_name(tool: Any) -> str | None:
    """Extract a tool name from a `BaseTool` or dict tool (mirrors the SDK)."""
    if isinstance(tool, dict):
        name = tool.get("name")
        return name if isinstance(name, str) else None
    name = getattr(tool, "name", None)
    return name if isinstance(name, str) else None


class ToolFilterMiddleware(AgentMiddleware[Any, Any, Any]):
    """Request-time allow/deny filter over a subagent's visible tools.

    Runs late in the middleware stack (it is appended last), so it sees the
    fully assembled tool list: consumer tools plus middleware-injected tools
    (filesystem, ``execute``, ``task``, MCP, ...). Exclusion wins over the
    allowlist when a name appears in both.
    """

    def __init__(
        self,
        *,
        allowed: list[str] | frozenset[str] | None = None,
        excluded: list[str] | frozenset[str] | None = None,
    ) -> None:
        super().__init__()
        self._allowed: frozenset[str] | None = (
            frozenset(allowed) if allowed is not None else None
        )
        self._excluded: frozenset[str] = frozenset(excluded or ())

    def _filter(self, tools: list[Any]) -> list[Any]:
        kept: list[Any] = []
        for tool in tools:
            name = _tool_name(tool)
            if name in self._excluded:
                continue
            if self._allowed is not None and name not in self._allowed:
                continue
            kept.append(tool)
        return kept

    def wrap_model_call(
        self,
        request: ModelRequest[Any],
        handler: Callable[[ModelRequest[Any]], ModelResponse[Any]],
    ) -> ModelResponse[Any]:
        if self._allowed is None and not self._excluded:
            return handler(request)
        return handler(request.override(tools=self._filter(request.tools)))

    async def awrap_model_call(
        self,
        request: ModelRequest[Any],
        handler: Callable[[ModelRequest[Any]], _Awaitable[ModelResponse[ResponseT]]],
    ) -> ModelResponse[ResponseT] | AIMessage | ExtendedModelResponse[ResponseT]:
        if self._allowed is None and not self._excluded:
            return await handler(request)
        return await handler(request.override(tools=self._filter(request.tools)))


def _norm_string_list(value: Any) -> list[str] | None:
    """Normalize a frontmatter field to a list of stripped non-empty strings.

    Returns `None` when the field is absent -- distinct from an explicit empty
    list, which is preserved (`tools: []` means "no tools").
    """
    if value is None:
        return None
    if isinstance(value, str):
        items = [value]
    elif isinstance(value, list):
        items = value
    else:
        logger.warning(
            "dcode_subagent_patch: expected a list or string for tool field, got %r", value
        )
        return None
    return [str(item).strip() for item in items if str(item).strip()]


def _norm_middleware_spec(value: Any) -> list[dict[str, Any]]:
    """Normalize `middleware:` into a list of `{name: str, **kwargs}` specs."""
    if value is None:
        return []
    if isinstance(value, str):
        value = [value]
    if not isinstance(value, list):
        logger.warning(
            "dcode_subagent_patch: expected a list for `middleware`, got %r", value
        )
        return []
    out: list[dict[str, Any]] = []
    for item in value:
        if isinstance(item, str):
            out.append({"name": item})
        elif isinstance(item, dict):
            spec = dict(item)
            name = spec.pop("name", None)
            if not isinstance(name, str) or not name.strip():
                logger.warning(
                    "dcode_subagent_patch: middleware entry %r has no string `name`", item
                )
                continue
            out.append({"name": name.strip(), **spec})
        else:
            logger.warning(
                "dcode_subagent_patch: middleware entry %r is neither a string nor a mapping",
                item,
            )
    return out


def _overrides_from_frontmatter(frontmatter: dict[str, Any]) -> dict[str, Any]:
    """Extract the three patched fields from a parsed frontmatter mapping."""
    return {
        "tools": _norm_string_list(frontmatter.get("tools")),
        "excluded_tools": _norm_string_list(frontmatter.get("excluded_tools")),
        "middleware": _norm_middleware_spec(frontmatter.get("middleware")),
    }


def _has_overrides(overrides: dict[str, Any]) -> bool:
    return (
        overrides["tools"] is not None
        or bool(overrides["excluded_tools"])
        or bool(overrides["middleware"])
    )


def _build_custom_middleware(spec: dict[str, Any]) -> Any:
    """Instantiate one custom middleware from its normalized registry spec."""
    name = spec["name"]
    factory = CUSTOM_MIDDLEWARE.get(name)
    if factory is None:
        msg = (
            f"Unknown subagent middleware {name!r}; register a factory via "
            "dcode_subagent_patch.register(...)"
        )
        raise KeyError(msg)
    kwargs = {k: v for k, v in spec.items() if k != "name"}
    return factory(**kwargs)


def _apply_overrides(spec: dict[str, Any]) -> dict[str, Any]:
    """Append tool-filter / custom middleware to a subagent spec (if any)."""
    overrides = _SUBAGENT_OVERRIDES.get(spec.get("name"))
    if overrides is None:
        return spec
    middleware = list(spec.get("middleware") or [])
    if overrides["tools"] is not None or overrides["excluded_tools"]:
        middleware.append(
            ToolFilterMiddleware(
                allowed=overrides["tools"],
                excluded=overrides["excluded_tools"],
            )
        )
    for mw_spec in overrides["middleware"]:
        middleware.append(_build_custom_middleware(mw_spec))
    if middleware:
        spec = dict(spec)
        spec["middleware"] = middleware
    return spec


def _patch_subagents() -> None:
    import deepagents_code.subagents as subagents_mod

    orig_parse = subagents_mod._parse_subagent_file

    def _parse_subagent_file_with_overrides(
        file_path: Path, *, fallback_name: str | None = None
    ) -> Any:
        meta = orig_parse(file_path, fallback_name=fallback_name)
        if meta is None:
            return None
        try:
            content = file_path.read_text(encoding="utf-8")
        except OSError:
            return meta
        match = re.match(r"^---\s*\n(.*?)\n---\s*\n?(.*)$", content, re.DOTALL)
        if not match:
            return meta
        try:
            frontmatter = yaml.safe_load(match.group(1)) or {}
        except yaml.YAMLError:
            return meta
        if not isinstance(frontmatter, dict):
            return meta
        overrides = _overrides_from_frontmatter(frontmatter)
        if _has_overrides(overrides):
            _SUBAGENT_OVERRIDES[meta["name"]] = overrides
        return meta

    subagents_mod._parse_subagent_file = _parse_subagent_file_with_overrides


def _patch_agent() -> None:
    import deepagents_code.agent as agent_mod

    orig_create_deep_agent = agent_mod.create_deep_agent

    def _create_deep_agent_with_subagent_overrides(*args: Any, **kwargs: Any) -> Any:
        specs = kwargs.get("subagents")
        if specs:
            kwargs = dict(kwargs)
            kwargs["subagents"] = [_apply_overrides(spec) for spec in specs]
        return orig_create_deep_agent(*args, **kwargs)

    _create_deep_agent_with_subagent_overrides._dcode_subagent_patch = True  # type: ignore[attr-defined]
    agent_mod.create_deep_agent = _create_deep_agent_with_subagent_overrides


def apply() -> None:
    """Install the patch. Idempotent. Called automatically at import."""
    global _applied
    if _applied:
        return
    _patch_subagents()
    _patch_agent()
    _applied = True


apply()
