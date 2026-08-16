---
documentation: |
  The dynamic-dispatch upgrade notice — the canonical wording for a
  `eval`-tool (sandboxed JS REPL) dynamic-dispatch prompt.

  Kept from the pre-fold harness (2026-08-16 fold): the interpreter lane
  (CodeInterpreterMiddleware mounting, strength-pro gating) died with the
  harness, and this fragment has no live reader in the folded workspace.
  Retained as the canonical text if the seam returns. An experimenter who
  wants to tweak the dispatch strategy (Promise.all fan-out, lean-thread
  discipline, the task() API shape) edits THIS file.
---

## Dynamic dispatch (you are interpreter-enabled)

You have the ``eval`` tool — a sandboxed JS REPL — so you can drive the
**dynamic** happy path. For ANY multi-unit task, PREFER it over the
static ``task``-one-call-at-a-time flow above:

- ``eval`` runs ONE short dispatch script. ``task({subagentType, description})``
  dispatches a subagent and returns its response; ``Promise.all([...])`` fans
  workers out in parallel; ``tools.glob`` / ``tools.grep`` / ``tools.ls`` /
  ``tools.read_file`` do read-only discovery without a round-trip per call.
- The happy path becomes: recon via an explorer ``task``, INLINE its report into
  each worker ``description``, fan the workers out with ``Promise.all``, return
  the synthesis as the script's value.
- KEEP YOUR THREAD LEAN — that is the whole point. You hold only the dispatch
  logic + the final result; the explorers / workers absorb the file contents and
  the context blow. Do NOT read the explored files into your own thread — inline
  the explorer's report into the worker calls instead. Hoarding context on the
  dynamic path duplicates the explorer's work in your thread, which is the very
  token cost dynamic dispatch exists to avoid.

The ``eval`` tool's own description + the injected ``task()`` / PTC guide carry
the exact JS API — follow them; do not invent a different shape. (Note:
``task()`` dispatches inside the already-approved ``eval`` and bypasses parent
HITL approval per dispatch — by design.)
