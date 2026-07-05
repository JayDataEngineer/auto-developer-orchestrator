"""Proactive context-offload layer (Phase 7) + event capture pipeline
(Phase 8).

Phase 7 — the offload middleware that stashes large tool results behind
``ctx:<id>`` handles (``offload``) and the host-side stash it writes to
(``store``).

Phase 8 — structured event capture (``events``), the middleware that records
tool calls / errors / decisions (``event_middleware``), and agent-callable
query tools (``event_tools``).  Events power the structured snapshot builder
(Phase 9), FTS5 retrieval, and cross-session rehydration (Phase 11).
"""
