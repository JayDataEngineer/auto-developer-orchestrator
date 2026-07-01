# PRESENT_AND_POST

How the CTO presents options to the user and triggers posting.

## Use ask_user With Structured Options

The `ask_user` tool blocks waiting for the user to pick from a list:

```python
ask_user({
    "question": "Which option should I post?",
    "options": [
        "A — contrarian take: \"Everyone says agents will replace devs...\"",
        "B — listicle thread: \"5 things I learned shipping agents...\"",
        "C — hot take: \"By end of 2026 every SaaS will ship an agent...\"",
        "D — Cancel, don't post anything"
    ],
    "default": "D — Cancel, don't post anything"
})
```

Rules:
- Provide at least 2 options (tool rejects 1 or 0)
- Include "Cancel" as the last option always
- Show the option letter + angle + first 60 chars of text in each option label
- The user can type free text if none of the options fit (handled by the tool)

## How It Appears in the TUI

The TUI renders `ask_user` as an interactive picker. The user uses arrow keys to select + Enter to confirm. The selected option text is returned to the agent.

## Routing the Selection

After ask_user returns:

| Selection | Action |
|-----------|--------|
| "A — ..." | Extract option A from bundle.json → delegate_to distribution-director |
| "B — ..." | Same for B |
| "Cancel..." | Stop. Mark task as cancelled. |
| Free text | Treat as new instructions — re-plan with the feedback |

## Reading the Bundle

The bundle path is `/sandbox/workspace/drafts/bundle.json`. Each option has:

```json
{
  "id": "A",
  "text": "..." or null,
  "thread": [...] or null,
  "image_path": "/sandbox/workspace/images/img_1.png" or null,
  "angle": "contrarian take",
  "platforms": ["twitter", "telegram"]
}
```

Pass the FULL option dict to the distribution-director, not just the ID.

## Pre-Flight Check

Before calling ask_user, verify:
1. `bundle.json` exists and parses
2. At least one option has either text or thread
3. Image paths exist on disk (if referenced)

If any check fails, surface the error to the user instead of ask_user.

## Post-Selection Handoff

```python
# After ask_user returns the user's pick:
delegate_to(
    role="distribution-director",
    task=f"User selected: {selection}. Read bundle.json, find the matching option, delegate to publisher with the option's text/thread/image_path and platforms list."
)
```

## Cancel Handling

If the user picks Cancel:
- Mark the SurrealDB task_run as `status: cancelled`
- Return a brief summary: "Pipeline cancelled at presentation. Research and drafts are at /sandbox/workspace/ for your review."
- Do NOT post anything

## User Feedback (Free Text)

If the user types something like "rewrite option B to be shorter":
- Don't re-run the full pipeline
- delegate_async to creative-writer with: "Revise option B per user feedback: shorter. Keep the same angle."
- collect_results → re-present
