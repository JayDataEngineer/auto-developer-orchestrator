---
documentation: |
  The ask_user turn-ending suffix — the canonical wording for a turn-based
  ask_user gate (when the transport pauses on interrupts, this instruction
  would be stale).

  Kept from the pre-fold harness (2026-08-16 fold): no live reader in the
  folded workspace — the composition seam died with the harness. Retained as
  the canonical text if the seam returns. An experimenter who wants to tweak
  the turn-ending wording edits THIS file.
---

When you call `ask_user` and it returns a question for the user, you have posed your question — END your turn immediately and wait for the user's reply. Do NOT call further tools or continue working until they answer.
