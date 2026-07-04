"""Proactive tool-output offload for the deepagents main agent (Phase 7).

Two halves, both harness-native (no external MCP dependency — see
``ctx_store.py`` for why context-mode isn't bridged):

* ``ContextOffloadMiddleware`` — an ``AgentMiddleware`` whose ``wrap_tool_call``
  measures each result; when a tool returns more than ``threshold`` chars, the
  full text is stashed to the ctx store and the ``ToolMessage`` the model
  actually sees is replaced with a short stub + a ``ctx:<id>`` retrieval handle.
  This is *proactive*: it keeps large results out of the context window before
  they accumulate, complementing deepagents' reactive ``SummarizationMiddleware``
  (which only evicts on overflow).

* ``ctx_recall`` / ``ctx_search`` — agent-callable tools to pull a stashed result
  back (by handle) or grep across all of them. Only the slice the agent asks for
  re-enters its context. They are **exempt** from offload (``_RETRIEVAL_TOOLS``):
  their purpose is to inject content, so re-stashing their output would trap the
  agent the instant it retrieves a large stash (proven in the Phase 7 E2E before
  the exemption).

The middleware runs on the **main agent** (the orchestrator that accumulates the
most context across a multi-step run). Subagent offload does NOT work today:
deepagents' ``SubAgentMiddleware`` builds each specialist via a compile path
that does not forward a raw spec's ``middleware`` key (the Phase 7 E2E confirmed
this — a researcher's ``read_file`` of an 11.9K-char file returned the full text
yet nothing was stashed). The store plumbing is tree-shared (``shared_store()``),
so enabling subagent offload later via ``CompiledSubAgent`` pre-compilation is
additive — but forcing it through the raw-spec key would be a shim, so it stays
out until done properly.
"""
from __future__ import annotations

from typing import Any, Callable

from langchain.agents.middleware.types import AgentMiddleware
from langchain_core.messages import ToolMessage
from langchain_core.tools import StructuredTool
from pydantic import BaseModel, Field

from pux_harness.context.store import CtxStore, SearchHit, StashResult, shared_store

# Defaults: ~2k tokens (chars/4). Tuned to catch directory dumps, big greps,
# log tails, and JSON blobs without trimming ordinary tool replies.
DEFAULT_THRESHOLD = 8000
DEFAULT_PREVIEW = 1500


def _is_text_tm(result: Any) -> bool:
    """Only offload a real ToolMessage with plain-string content. Multimodal
    content (lists of blocks), Command returns, and non-ToolMessage results pass
    through untouched — offloading structured/image output would lose it."""
    return (
        isinstance(result, ToolMessage)
        and isinstance(result.content, str)
    )


# ctx_recall / ctx_search are the *retrieval* surface — their whole job is to
# bring (a slice of) stashed content back INTO context. Offloading their output
# would re-stash it the instant the agent retrieves it (proven in the Phase 7
# E2E: ctx_recall(12087B) returned 10087B which got offloaded again, trapping
# the agent). So they are exempt: when the agent asks for the bytes, it gets them.
_RETRIEVAL_TOOLS = frozenset({"ctx_recall", "ctx_search"})


def _stub(stash: StashResult, content: str, tool: str, *, preview: int) -> str:
    head = content[:preview]
    ellipsis = "…" if len(content) > preview else ""
    return (
        f"[ctx-offload] {tool} returned {stash.chars} bytes — stashed as "
        f"{stash.handle} so it doesn't crowd the working context. Retrieve the "
        f"full output with ctx_recall({stash.handle!r}), or grep across every "
        f"stashed result with ctx_search(\"<a distinctive phrase>\").\n\n"
        f"Preview (first {preview} chars):\n{head}{ellipsis}"
    )


def _offload(
    result: Any, store: CtxStore, tool_name: str, *, threshold: int, preview: int
) -> Any:
    """If ``result`` is an oversized text ToolMessage, stash + replace. Else
    return unchanged. Pure (no I/O beyond the store); called by both the sync
    and async wrap hooks so behavior is identical either way.

    ``threshold <= 0`` is a kill-switch: offload nothing (handy for tests + a
    future env-flag to disable the feature without unwiring the middleware).

    Retrieval tools (``ctx_recall``/``ctx_search``) are exempt — see
    ``_RETRIEVAL_TOOLS``."""
    if (
        threshold <= 0
        or tool_name in _RETRIEVAL_TOOLS
        or not _is_text_tm(result)
        or len(result.content) <= threshold
    ):
        return result
    stash = store.stash(result.content, tool=tool_name)
    return ToolMessage(
        content=_stub(stash, result.content, tool_name, preview=preview),
        tool_call_id=result.tool_call_id,
        name=result.name,
    )


class ContextOffloadMiddleware(AgentMiddleware):
    """Offload oversized tool results to the ctx store, replacing them with a
    preview + retrieval handle. Set ``threshold=0`` to disable (offload nothing).
    """

    def __init__(
        self,
        store: CtxStore | None = None,
        *,
        threshold: int = DEFAULT_THRESHOLD,
        preview: int = DEFAULT_PREVIEW,
    ) -> None:
        self.store = store or shared_store()
        self.threshold = threshold
        self.preview = preview

    def _tool_name(self, request: Any) -> str:
        tc = getattr(request, "tool_call", None) or {}
        return tc.get("name") if isinstance(tc, dict) else "tool"

    def wrap_tool_call(self, request, handler):  # type: ignore[no-untyped-def]
        result = handler(request)
        return _offload(
            result, self.store, self._tool_name(request),
            threshold=self.threshold, preview=self.preview,
        )

    async def awrap_tool_call(self, request, handler):  # type: ignore[no-untyped-def]
        result = await handler(request)
        return _offload(
            result, self.store, self._tool_name(request),
            threshold=self.threshold, preview=self.preview,
        )


# --- agent-callable tools -----------------------------------------------------


class _RecallArgs(BaseModel):
    handle: str = Field(
        ..., description='A ctx handle like "ctx:1a2b3c4d5e6f" returned by an offload stub.'
    )


class _SearchArgs(BaseModel):
    query: str = Field(..., description="A distinctive phrase to grep for across stashed results.")
    limit: int = Field(5, description="Max hits to return (default 5).")


def _format_hits(hits: list[SearchHit]) -> str:
    if not hits:
        return "no stashed result matched that query."
    lines = [f"{len(hits)} hit(s):"]
    for h in hits:
        tag = f"[{h.tool}]" if h.tool else ""
        lab = f" {h.label}" if h.label else ""
        lines.append(f"- {h.handle}{tag}{lab}: {h.snippet}")
    return "\n".join(lines)


def build_ctx_tools(store: CtxStore | None = None) -> list[StructuredTool]:
    """The ``ctx_recall`` + ``ctx_search`` tools, bound to ``store`` (default:
    the process-wide shared store). Built fresh per call so a test can pass its
    own ``CtxStore(tmp_path)`` and have offload + recall share it."""
    s = store or shared_store()

    def _recall(handle: str) -> str:
        out = s.recall(handle)
        return out if out is not None else f"no stashed content for handle {handle!r}"

    def _search(query: str, limit: int = 5) -> str:
        return _format_hits(s.search(query, limit=limit))

    recall = StructuredTool.from_function(
        _recall,
        name="ctx_recall",
        description=(
            "Retrieve the FULL content of a previously offloaded tool result by its "
            'ctx handle (e.g. "ctx:1a2b3c4d5e6f"). Use when a tool said its output '
            "was stashed and you need more than the preview."
        ),
        args_schema=_RecallArgs,
    )
    search = StructuredTool.from_function(
        _search,
        name="ctx_search",
        description=(
            "Grep across every offloaded tool result for a phrase; returns matching "
            "handles + a snippet each, then use ctx_recall to pull a full hit. Use "
            "when you remember a detail but not which call produced it."
        ),
        args_schema=_SearchArgs,
    )
    return [recall, search]
