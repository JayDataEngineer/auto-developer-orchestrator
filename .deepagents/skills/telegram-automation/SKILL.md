---
name: telegram-automation
description: Drive Telegram via Telethon (MTProto) and the telegram_helpers Python library — post to Saved Messages, send/read/search chats, and bootstrap a session. Use when the Telegram agent posts a note, reads mentions, sends a message, or searches history.
---

# TELEGRAM_HELPERS — Python API

Library of Telegram helpers for agent-written scripts. Import from `telegram_helpers`.

All functions raise `RuntimeError` with a clear message if the session is missing or invalid. Run `python3 .deepagents/skills/telegram-automation/scripts/telegram_session.py --check` to diagnose.

## Setup (one-time)

```bash
python3 .deepagents/skills/telegram-automation/scripts/telegram_session.py --setup-credentials API_ID API_HASH +PHONE
python3 .deepagents/skills/telegram-automation/scripts/telegram_session.py --bootstrap     # interactive SMS auth
python3 .deepagents/skills/telegram-automation/scripts/telegram_session.py --check         # verify
```

Get api_id + api_hash from https://my.telegram.org/apps.

## Function reference

### `post_to_saved_messages(text: str) -> dict`
Post to your own Saved Messages. The killer feature — use Telegram as a notes surface.

```python
from telegram_helpers import post_to_saved_messages
post_to_saved_messages("buy milk")
post_to_saved_messages("**PROJECT LOG**\n- shipped X\n- blocked on Y")
```

Returns: `{"ok": True, "message_id": 123, "sent_at": "...", "char_count": 8}`

Telegram markdown supported: `**bold**`, `*italic*`, `` `code` ``, `[link](url)`.

---

### `send_message(chat: str, text: str) -> dict`
Send to any chat — user, group, channel, or `'me'` for Saved Messages.

```python
send_message("@somefriend", "hi from the agent")
send_message("me", "private note")  # equivalent to post_to_saved_messages
send_message("+15551234567", "SMS-style addressing works too")
```

`chat` accepts: `@username`, `+phone`, integer chat ID, or `'me'`.

Returns: `{"ok": True, "message_id": 123, "target": "@somefriend", "sent_at": "..."}`

---

### `read_messages(chat: str, limit: int = 20) -> list[dict]`
Read recent messages from a chat. Most recent first.

```python
msgs = read_messages("me", limit=10)
for m in msgs:
    print(f"{m['date']} {m['sender']}: {m['text']}")
```

Returns: list of dicts with `message_id`, `sender` (display name, not phone), `text`, `date` (ISO).

`text` is empty string for media-only messages. Limit capped at 100.

---

### `list_chats(limit: int = 20) -> list[dict]`
List recent conversations. Most recent first.

```python
for c in list_chats(limit=15):
    if c["unread_count"] > 0:
        print(f"{c['name']}: {c['unread_count']} unread")
```

Returns: list of dicts with `name`, `chat_id`, `username`, `last_message_date`, `unread_count`.

---

### `search_messages(query: str, chat: Optional[str] = None, limit: int = 20) -> list[dict]`
Search by keyword. Optional chat filter.

```python
results = search_messages("meeting")
results = search_messages("invoice", chat="me", limit=5)
```

Returns: list of dicts with `message_id`, `sender`, `text`, `date`, `chat_id`.

---

### `telegram_session()` — context manager
Lower-level. Use when you need Telethon directly.

```python
from telegram_helpers import telegram_session

with telegram_session() as client:
    me = client.get_me()
    print(me.first_name)
    # Any Telethon call works here:
    dialogs = client.iter_dialogs()
    for d in dialogs:
        print(d.name)
```

Yields a connected, authorized `TelegramClient`. Disconnects on exit. Uses Telethon sync mode — no `await` needed.

---

### `has_valid_session() -> bool`
Quick file-presence check. Doesn't call the API. Use for cheap guards.

```python
if not has_valid_session():
    print("Session not bootstrapped. Run session.py --bootstrap first.")
```

### `load_credentials() -> dict`
Read `data/.telegram-credentials.json`. Returns `{api_id, api_hash, phone, saved_at}`.

## Common patterns

### Daily journal entry
```python
from telegram_helpers import post_to_saved_messages
from datetime import datetime

entry = f"""
**Daily log — {datetime.now().strftime('%Y-%m-%d')}**

- shipped X
- learned Y
- blocked on Z
"""
post_to_saved_messages(entry.strip())
```

### Read last 5 unread from Saved Messages
```python
from telegram_helpers import read_messages
for m in read_messages("me", limit=5):
    print(f"{m['date'][:10]} {m['text']}")
```

### Find notes by keyword
```python
from telegram_helpers import search_messages
for r in search_messages("idea", chat="me", limit=10):
    print(f"{r['date'][:10]}: {r['text'][:80]}")
```

## Privacy rules

- Sender display name only — never phone numbers
- Don't log message bodies unless explicitly asked
- Return counts + IDs by default
