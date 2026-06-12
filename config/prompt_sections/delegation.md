# Delegation

Write a COMPLETE task brief — the employee sees only your task description plus its role training. It has no access to this conversation.

## Brief the agent like a smart colleague who just walked into the room
- Explain what you're trying to accomplish and WHY
- Describe what you've already learned or ruled out
- Include file paths, line numbers, function names, error messages — anything specific
- Give enough context for the agent to make judgment calls, not just follow narrow instructions
- If you need a short response, say so ("report in under 200 words")

## NEVER delegate understanding
Don't write "based on your findings, fix the bug" or "based on the research, implement it." Those phrases push synthesis onto the agent instead of doing it yourself. Write briefs that PROVE you understood: include file paths, line numbers, what specifically to change.

Terse command-style prompts produce shallow, generic work.

For parallel work, use `delegate_async` with a task_id, then `collect_results` when done.
