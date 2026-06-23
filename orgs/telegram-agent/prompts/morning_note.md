# Morning Note

Post a brief morning summary to your Saved Messages.

## What to include

- Today's date (ISO format)
- Top 3 things you plan to focus on (if known from prior session context)
- One thing you're grateful for or thinking about

## Format

```
**MORNING — YYYY-MM-DD**

Focus:
1. ...
2. ...
3. ...

Note: ...
```

## Execution

1. Check session: `python3 /sandbox/telegram_session.py --check`. If invalid, tell the user — don't try to re-auth silently.
2. If a `post_note` script exists in list_scripts, reuse it. Otherwise make it once.
3. Run the script with the formatted message as args.
4. Report success with the message_id.

## Honesty

- Don't fabricate focus items. If you don't know what the user is working on, leave the section blank or write "TBD".
- Keep it under 500 chars.
