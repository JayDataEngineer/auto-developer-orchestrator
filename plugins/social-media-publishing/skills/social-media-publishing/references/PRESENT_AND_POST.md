# PRESENT_AND_POST

How the CTO presents options to the user and triggers posting.

## Use ask_user With Structured Options

`ask_user` poses a question to the human and blocks until they reply. The reply
text is returned to the agent. Signature (separate args):

```python
ask_user(
    question="Which option should I post?",
    options=[
        "A — contrarian take: \"Everyone says agents will replace devs...\"",
        "B — listicle thread: \"5 things I learned shipping agents...\"",
        "C — hot take: \"By end of 2026 every SaaS will ship an agent...\"",
        "D — Cancel, don't post anything",
    ],
    default="D — Cancel, don't post anything",
)
```

Rules:
- `options` is optional — pass a list when the choices are enumerable, or omit it
  to ask an open question (the human types a free reply).
- When listing options, include "Cancel" as the last one.
- Show the option letter + angle + first ~60 chars of text in each option label.
- The human is never locked to a listed option — over the web they can type free
  text in the card; over the editor their next message can be anything.

## How It Reaches the Human (transport-aware)

There is no in-harness picker UI. `ask_user` is transport-aware:

- **Web (AG-UI / CopilotKit):** the call interrupts the run. CopilotKit's
  `useInterrupt` card surfaces the question + options; the human's reply
  (a selected option or typed text) resumes the run and becomes the tool's
  return value.
- **Editor (Zed/Toad):** the editor's permission popover has no free-text
  field, so the tool poses the question as chat text and ENDS the turn. The
  human's next message is the reply. The supervisor prompt makes you stop after
  asking — do not call further tools until they answer.

In both cases the tool returns the human's reply as plain text; route it per the
table below.

## Routing the Selection

After ask_user returns:

| Selection | Action |
|-----------|--------|
| "A — ..." | Extract option A from bundle.json → delegate_to distribution-director |
| "B — ..." | Same for B |
| "Cancel..." | Stop. Mark task as cancelled. |
| Free text | Treat as new instructions — re-plan with the feedback |

## Reading the Bundle

The bundle path is `drafts/bundle.json`. Each option has:

```json
{
  "id": "A",
  "text": "..." or null,
  "thread": [...] or null,
  "image_path": "images/img_1.png" or null,
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
- Return a brief summary: "Pipeline cancelled at presentation. Research and drafts are at  for your review."
- Do NOT post anything

## User Feedback (Free Text)

If the user types something like "rewrite option B to be shorter":
- Don't re-run the full pipeline
- delegate_async to creative-writer with: "Revise option B per user feedback: shorter. Keep the same angle."
- collect_results → re-present
