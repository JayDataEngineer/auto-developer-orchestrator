"""Host-side context stash for proactive tool-output offload (Phase 7).

When a tool returns more text than the agent's working context should carry,
``ContextOffloadMiddleware`` parks the full output here and hands the agent a
small ``ctx:<id>`` handle instead. The agent pulls the bytes back on demand via
the ``ctx_recall`` / ``ctx_search`` tools — only the slice it needs re-enters
its context. This is the *proactive* complement to deepagents' own reactive
``SummarizationMiddleware`` (which only offloads once the window has *already*
overflowed); together they keep large tool results from dominating the prompt.

Why host-side (not the sandbox backend's ``/large_tool_results/``): the
middleware runs in the harness process and the store is a harness-owned cache of
results that already came back to it. Plain file I/O — no Docker, no backend
handle, no network — so it is trivially testable and decoupled from the sandbox
lifecycle. ``.pux/`` is gitignored (it already holds the agent-protocol sqlite
+ mcpserver pid), so stashed output never leaks into version control.

The store only ever holds *tool results the harness already received* — it is a
cache, not a new channel. It does not give the agent any host-write capability
it didn't already have.
"""
from __future__ import annotations

import json
import os
import re
import uuid
from dataclasses import dataclass, field
from pathlib import Path

from pux_harness.orgs import PROJECT_ROOT

CTX_ROOT = PROJECT_ROOT / ".pux" / "ctx"
_HANDLE_RE = re.compile(r"^ctx:(?P<id>[0-9a-f]+)$")


@dataclass(frozen=True)
class StashResult:
    """What ``stash`` hands back to the middleware + agent."""

    handle: str  # "ctx:<id>"
    id: str  # bare id
    chars: int


@dataclass(frozen=True)
class SearchHit:
    handle: str
    tool: str
    label: str
    snippet: str


@dataclass(frozen=True)
class _Meta:
    tool: str = ""
    label: str = ""
    chars: int = 0


class CtxStore:
    """Append-only on-disk stash. ``<root>/<id>.txt`` holds content;
    ``<root>/<id>.json`` holds ``{tool, label, chars}``."""

    def __init__(self, root: Path | str) -> None:
        self.root = Path(root)
        self.root.mkdir(parents=True, exist_ok=True)

    def _new_id(self) -> str:
        return uuid.uuid4().hex[:12]

    def stash(self, content: str, *, tool: str = "", label: str = "") -> StashResult:
        """Park ``content``; return its ``ctx:<id>`` handle. Ids are unique per
        call (uuid4) so two oversized results from the same tool never collide."""
        sid = self._new_id()
        (self.root / f"{sid}.txt").write_text(content, encoding="utf-8")
        (self.root / f"{sid}.json").write_text(
            json.dumps({"tool": tool, "label": label, "chars": len(content)}),
            encoding="utf-8",
        )
        return StashResult(handle=f"ctx:{sid}", id=sid, chars=len(content))

    def _meta_for(self, sid: str) -> _Meta:
        try:
            d = json.loads((self.root / f"{sid}.json").read_text(encoding="utf-8"))
            return _Meta(tool=d.get("tool", ""), label=d.get("label", ""), chars=d.get("chars", 0))
        except (FileNotFoundError, json.JSONDecodeError):
            return _Meta()

    def recall(self, handle: str) -> str | None:
        """Return the full content for ``ctx:<id>`` (or a bare id), else None.
        Missing files → None (not an error): a bad/garbage handle is a normal
        agent mistake, surfaced as 'not found' text to the model."""
        sid = _strip_handle(handle)
        if sid is None:
            return None
        try:
            return (self.root / f"{sid}.txt").read_text(encoding="utf-8")
        except FileNotFoundError:
            return None

    def search(self, query: str, *, limit: int = 5, window: int = 240) -> list[SearchHit]:
        """Case-insensitive substring scan across every stashed blob. Each hit
        returns a ``window``-char snippet around the first match. Linear scan is
        fine — the stash is a per-project working set, not a corpus. Empty query
        returns nothing (no accidental full dumps)."""
        if not query.strip():
            return []
        needle = query.lower()
        hits: list[SearchHit] = []
        for txt in sorted(self.root.glob("*.txt"), reverse=True):
            try:
                body = txt.read_text(encoding="utf-8")
            except OSError:
                continue
            idx = body.lower().find(needle)
            if idx < 0:
                continue
            start = max(0, idx - window // 2)
            end = min(len(body), idx + len(query) + window // 2)
            snippet = body[start:end]
            if start > 0:
                snippet = "…" + snippet
            if end < len(body):
                snippet = snippet + "…"
            meta = self._meta_for(txt.stem)
            hits.append(SearchHit(
                handle=f"ctx:{txt.stem}", tool=meta.tool, label=meta.label, snippet=snippet,
            ))
            if len(hits) >= limit:
                break
        return hits


def _strip_handle(handle: str) -> str | None:
    """Accept ``ctx:<id>`` or a bare ``<id>``; reject anything that isn't a hex
    id so the store path can't be escaped (``..``/``/`` etc.)."""
    if not handle:
        return None
    m = _HANDLE_RE.match(handle.strip())
    sid = m.group("id") if m else handle.strip()
    # Hex-only, fixed-ish length range — no path separators, no dots.
    if not re.fullmatch(r"[0-9a-f]{6,32}", sid):
        return None
    return sid


_store: CtxStore | None = None


def shared_store() -> CtxStore:
    """One process-wide store at ``<project>/.pux/ctx/``. Created lazily so
    importing this module never touches the disk (keeps ``--help`` + tests that
    build their own ``CtxStore(tmp_path)`` cheap + hermetic)."""
    global _store
    if _store is None:
        _store = CtxStore(os.environ.get("PUX_CTX_ROOT", CTX_ROOT))
    return _store
