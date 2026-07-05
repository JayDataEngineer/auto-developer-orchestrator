"""Event capture middleware — intercepts tool calls and model turns to
record structured events in the EventStore (Phase 8).

This middleware sits in the deepagents middleware stack (via ``wrap_tool_call``
and ``wrap_model_call``) and writes events fire-and-forget — never blocks the
agent.  It captures:

- Every tool call + result (P2 ``tool_call`` event with timing and success)
- Tool errors (P1 ``error`` event)
- Model turns (P4 ``session_start``/``session_end`` bookkeeping)

Downstream consumers (snapshot builder, FTS5 retrieval, Session Guide) read
these events to reconstruct working state after compaction.
"""
from __future__ import annotations

import json
import time
from typing import Any

from langchain.agents.middleware.types import AgentMiddleware
from langchain_core.messages import ToolMessage

from pux_harness.context.events import EventStore, shared_event_store

# Tools whose output we don't want to log verbatim (too large / redundant).
_SKIP_DETAIL_TOOLS = frozenset({"ctx_recall", "ctx_search", "event_recent", "event_query"})


class EventCaptureMiddleware(AgentMiddleware):
    """Capture structured events from every tool call.

    Set ``enabled=False`` to disable (useful for tests).
    """

    def __init__(
        self,
        store: EventStore | None = None,
        *,
        enabled: bool = True,
    ) -> None:
        self.store = store or shared_event_store()
        self.enabled = enabled

    def _tool_name(self, request: Any) -> str:
        tc = getattr(request, "tool_call", None) or {}
        return tc.get("name") if isinstance(tc, dict) else "tool"

    def _tool_args_summary(self, request: Any) -> str:
        """Truncated args string for the event payload."""
        tc = getattr(request, "tool_call", None) or {}
        args = tc.get("args", {}) if isinstance(tc, dict) else {}
        raw = json.dumps(args, ensure_ascii=False, default=str)
        return raw[:500] if len(raw) > 500 else raw

    def _thread_id(self, request: Any) -> str:
        """Extract thread_id from state if available."""
        state = getattr(request, "state", None) or {}
        if isinstance(state, dict):
            return state.get("configurable", {}).get("thread_id", "")
        return ""

    # -- sync ------------------------------------------------------------------

    def wrap_tool_call(self, request, handler):  # type: ignore[no-untyped-def]
        if not self.enabled:
            return handler(request)

        tool = self._tool_name(request)
        args_summary = self._tool_args_summary(request)
        thread_id = self._thread_id(request)
        t0 = time.time()

        try:
            result = handler(request)
        except Exception as exc:
            # Capture error event, then re-raise.
            elapsed = time.time() - t0
            self.store.capture(
                "error",
                {
                    "tool": tool,
                    "args": args_summary,
                    "error": str(exc),
                    "error_type": type(exc).__name__,
                    "elapsed_s": round(elapsed, 3),
                },
                thread_id=thread_id,
            )
            self.store.flush()
            raise

        elapsed = time.time() - t0
        success = True
        output_preview = ""

        if isinstance(result, ToolMessage):
            content = result.content if isinstance(result.content, str) else ""
            success = result.status != "error"
            if tool not in _SKIP_DETAIL_TOOLS:
                output_preview = content[:300] if content else ""

        self.store.capture(
            "tool_call",
            {
                "tool": tool,
                "args": args_summary,
                "success": success,
                "elapsed_s": round(elapsed, 3),
                "output_preview": output_preview,
            },
            thread_id=thread_id,
        )
        self.store.flush()
        return result

    # -- async -----------------------------------------------------------------

    async def awrap_tool_call(self, request, handler):  # type: ignore[no-untyped-def]
        if not self.enabled:
            return await handler(request)

        tool = self._tool_name(request)
        args_summary = self._tool_args_summary(request)
        thread_id = self._thread_id(request)
        t0 = time.time()

        try:
            result = await handler(request)
        except Exception as exc:
            elapsed = time.time() - t0
            self.store.capture(
                "error",
                {
                    "tool": tool,
                    "args": args_summary,
                    "error": str(exc),
                    "error_type": type(exc).__name__,
                    "elapsed_s": round(elapsed, 3),
                },
                thread_id=thread_id,
            )
            self.store.flush()
            raise

        elapsed = time.time() - t0
        success = True
        output_preview = ""

        if isinstance(result, ToolMessage):
            content = result.content if isinstance(result.content, str) else ""
            success = result.status != "error"
            if tool not in _SKIP_DETAIL_TOOLS:
                output_preview = content[:300] if content else ""

        self.store.capture(
            "tool_call",
            {
                "tool": tool,
                "args": args_summary,
                "success": success,
                "elapsed_s": round(elapsed, 3),
                "output_preview": output_preview,
            },
            thread_id=thread_id,
        )
        self.store.flush()
        return result
