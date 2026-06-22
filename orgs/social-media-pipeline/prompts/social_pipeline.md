Run the full social media pipeline.

User request: {message}

Follow the MANIFESTO 4-phase loop:
1. START — log task_run in SurrealDB
2. RESEARCH — delegate_async to research-director
3. DRAFT — delegate_async to content-director
4. PRESENT — ask_user with structured options
5. EXECUTE (if user picks an option) — delegate_to distribution-director
6. COMPLETE — mark task_run with outcome

Default mode: `Base`. Switch to `Lightning` only if user explicitly asks for quick.
