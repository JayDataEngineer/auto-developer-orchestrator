# TELEGRAM_RESEARCH

How to read Telegram messages via the Telethon session.

## Session Bootstrap (one-time)

```bash
# 1. Get API credentials from https://my.telegram.org/apps
python3 /sandbox/telegram_session.py --setup-credentials API_ID API_HASH PHONE

# 2. Bootstrap (sends SMS, you enter code, ~30 seconds)
python3 /sandbox/telegram_session.py --bootstrap

# 3. Verify
python3 /sandbox/telegram_session.py --check
```

Session persists at `/sandbox/.telegram-session.session` (SQLite file). Survives across runs.

## Read Messages

```bash
# List available chats
python3 /sandbox/telegram_helpers.py list-chats --limit 50

# Read recent messages from a chat
python3 /sandbox/telegram_helpers.py read-messages \
    --chat "Saved Messages" --limit 100

# Search messages
python3 /sandbox/telegram_helpers.py search-messages \
    --query "AI agents" --limit 20
```

## Python API

If you need more control, use the helpers directly:

```python
from telegram_helpers import (
    telegram_session,
    list_chats,
    read_messages,
    search_messages,
    send_message,
    post_to_saved_messages,
)

with telegram_session() as client:
    chats = list_chats(client, limit=50)
    for chat in chats[:5]:
        msgs = read_messages(client, chat, limit=50)
        for m in msgs:
            if interesting(m):
                yield m
```

## Interesting Messages Heuristics

A message is "interesting" if any of:
- Has ≥3 reactions (👍 ❤️ 🔥 etc.)
- Was forwarded from another channel (look for `fwd_from` field)
- Contains a URL to an article/video
- Has media attached (photo, video, document)
- Replies to a high-engagement message

## Don't Read

- Private 1:1 DMs (privacy concern)
- Secret chats (won't work anyway — E2E encrypted)
- Messages older than 30 days unless explicitly requested

## Output Format

Write to `/sandbox/workspace/research/telegram.json`. See telegram-researcher role prompt for the schema.
