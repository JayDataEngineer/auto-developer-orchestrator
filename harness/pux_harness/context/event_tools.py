"""Agent-callable tools for querying the event store (Phase 8).

These tools let the agent inspect its own captured history — recent tool
calls, errors, decisions, blockers — without re-reading the full message
stream.  BM25-ranked search (via FTS5) means the agent pulls back only the
relevant slice, keeping token cost low.
"""
from __future__ import annotations

from typing import Any

from langchain_core.tools import StructuredTool
from pydantic import BaseModel, Field

from pux_harness.context.events import EventStore, shared_event_store


class _RecentArgs(BaseModel):
    event_type: str = Field(
        "",
        description=(
            "Filter by event type (e.g. 'tool_call', 'error', 'decision_made'). "
            "Empty string returns all types."
        ),
    )
    limit: int = Field(15, description="Max events to return (default 15).")


class _QueryArgs(BaseModel):
    query: str = Field(
        ...,
        description=(
            "Search phrase for BM25-ranked retrieval across event data. "
            "Returns results ordered by relevance."
        ),
    )
    limit: int = Field(10, description="Max hits to return (default 10).")


def _format_event(ev: Any) -> str:
    """One-line summary for an event."""
    import datetime

    ts_str = datetime.datetime.fromtimestamp(ev.ts, tz=datetime.timezone.utc).strftime(
        "%H:%M:%S"
    )
    data_str = ""
    if ev.data:
        # Compact: drop output_preview from display if present.
        display = {k: v for k, v in ev.data.items() if k != "output_preview"}
        data_str = f" :: {display}"
    return f"[{ts_str}] P{ev.priority} {ev.type}{data_str}"


def build_event_tools(store: EventStore | None = None) -> list[StructuredTool]:
    """Build the ``event_recent`` + ``event_query`` tools."""
    s = store or shared_event_store()

    def _recent(event_type: str = "", limit: int = 15) -> str:
        events = s.recent(event_type=event_type, limit=limit)
        if not events:
            return "no events recorded yet."
        lines = [f"{len(events)} event(s), newest first:"]
        for ev in events:
            lines.append(f"- {_format_event(ev)}")
        return "\n".join(lines)

    def _query(query: str, limit: int = 10) -> str:
        events = s.query(query, limit=limit)
        if not events:
            return f"no events matched '{query}'."
        lines = [f"{len(events)} hit(s) for '{query}':"]
        for ev in events:
            lines.append(f"- {_format_event(ev)}")
        return "\n".join(lines)

    recent = StructuredTool.from_function(
        _recent,
        name="event_recent",
        description=(
            "Show recent captured events (tool calls, errors, decisions, blockers). "
            "Optionally filter by type. Events are from the current session's "
            "capture pipeline — they include structured metadata that the raw "
            "message history doesn't preserve (timing, success, error details)."
        ),
        args_schema=_RecentArgs,
    )
    query = StructuredTool.from_function(
        _query,
        name="event_query",
        description=(
            "BM25-ranked search across all captured events. Returns results "
            "ordered by relevance. Use when you remember a detail but not when "
            "it happened — e.g. 'blocked on auth' or 'rejected approach flask'."
        ),
        args_schema=_QueryArgs,
    )
    return [recent, query]
