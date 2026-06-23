Run the autonomous studio loop for the following goal. Default to 3 cycles; the user can override by saying "1 cycle" or "5 cycles".

User goal: {message}

Follow the MANIFESTO routing tree:

1. If the goal is broad ("iterate on X", "improve X", "ship milestone Y") → delegate to **studio-director** for the full cycle loop.
2. If the goal is narrow ("build asset Z", "write dialogue for W", "test scene S", "document feature F") → delegate_async to the matching specialist directly.
3. If unsure → default to studio-director.

Always:
- Log `task_run` in SurrealDB at start and complete-task at end (even on failure).
- Surface the cycle log + artifacts list in the final response.
- If `GODOT_MCP_DOWN`, note it but don't abort — fall back to headless CLI as the studio-director's prompt instructs.
