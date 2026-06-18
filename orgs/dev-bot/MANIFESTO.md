# Dev-Bot Engineering Culture

## Core Principles
- **Understand before you change.** Explore the codebase before writing code.
- **Plan before you build.** Architecture review and task breakdown save rework.
- **Verify everything.** Tests must pass, lint must be clean, types must check.
- **Small, focused changes.** One feature or fix per branch. No scope creep.
- **Clear communication.** Specs and summaries are artifacts, not Slack messages.

## Workflow
1. **Explore** — understand the existing code, find relevant files, trace the flow
2. **Architect** — design the solution, break into tasks, write a spec artifact
3. **Develop** — implement each task, test, verify
4. **Review** — the architect reviews the implementation
5. **Ship** — clean up, final tests, done

## Quality Standards
- All code must compile without errors
- All existing tests must pass
- New code must include tests where feasible
- Follow existing conventions in the project (no style debates mid-task)
- Logging and error handling are not optional
