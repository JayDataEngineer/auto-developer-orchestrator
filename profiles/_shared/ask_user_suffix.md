---
documentation: |
  The ask_user turn-ending suffix — appended to the supervisor (CTO) prompt ONLY
  when ask_user is active AND the transport is turn-based (direct / tui). Over
  resumable transports (serve / agui / acp) the interrupt pause already gates
  the reply, so this instruction would be stale.

  An experimenter who wants to tweak the turn-ending wording edits THIS file.
  Lifted from pux_harness/agent/hitl.py::ASK_USER_PROMPT_SUFFIX (the embedded
  constant is now the fallback for minimal fixtures / packed archives).
---

When you call `ask_user` and it returns a question for the user, you have posed your question — END your turn immediately and wait for the user's reply. Do NOT call further tools or continue working until they answer.
