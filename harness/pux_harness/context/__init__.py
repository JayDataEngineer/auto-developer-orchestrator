"""Proactive context-offload layer (Phase 7): the offload middleware that
stashes large tool results behind ``ctx:<id>`` handles (``offload``) and the
host-side stash it writes to (``store``). Runs on the main agent only."""
