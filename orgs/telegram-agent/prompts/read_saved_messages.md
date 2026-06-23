# Read Saved Messages

Pull the last N notes from Saved Messages so the user can see what they wrote recently.

## Steps

1. Check session: `python3 /sandbox/telegram_session.py --check`. Escalate if invalid.
2. Use `read_messages` from `telegram_helpers`:

```python
from telegram_helpers import read_messages
msgs = read_messages("me", limit=10)
for m in msgs:
    print(f"{m['date'][:16]} {m['text'][:120]}")
```

3. Summarize: how many notes, dates range, top 3 themes (if obvious).
4. If user asks for specific topic, use `search_messages(topic, chat="me")` instead.

## Output

Return a compact summary: count + dates + first line of each note. Don't dump full bodies unless asked.
