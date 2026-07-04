---
name: source-citation
description: Cite file:line evidence for every factual claim. Use when answering questions about a codebase or running investigation results back to the operator.
---

# Source Citation

Every factual claim in a final response must trace back to a specific file
and line you actually read. Quoting from memory or training data is not
evidence.

## Pattern

- Reference code as `harness/pux_harness/agent/orgs.py:70` so the operator can jump to it.
- When summarizing a function, name the file and the line range:
  `harness/pux_harness/agent/orgs.py:196-227`.
- When listing matches from `grep`, paste the matched line + path, not
  just the count.
- If you're citing an external source (docs, web), include the URL.

## When NOT to apply

- For your own reasoning or plans — those are inferences, not facts.
- For tool output verbatim — the tool result is its own evidence.
- For trivial statements ("the workspace has files", "bash ran successfully").

## Why

The operator reads your final message and decides what to do next. If your
facts are uncited, they have to re-do your investigation to trust them. If
they're cited, they can verify in one keystroke and act immediately.
