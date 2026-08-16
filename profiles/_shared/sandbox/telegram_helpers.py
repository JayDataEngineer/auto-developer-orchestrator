"""
Telegram automation helpers — shared library for agent-written scripts.

Wraps the Telethon auth dance so that ad-hoc scripts (post_note.py,
read_mentions.py, etc.) can be 5-10 lines instead of 50. Designed to be
obvious for fast/cheap models like DeepSeek V4 Flash — explicit kwargs,
no cleverness, full docstrings.

Unlike twitter_helpers (which injects cookies into a SeleniumBase session),
Telegram uses MTProto via Telethon. The session state lives in a SQLite
file at /sandbox/workspace/data/.telegram-session.session — populated once by
`python3 /sandbox/session.py --bootstrap` and reused forever.

Usage in agent scripts:

    from telegram_helpers import post_to_saved_messages, telegram_session

    # One-liner: post a note to yourself
    post_to_saved_messages("remember to buy milk")

    # Session context: do multiple ops on one connection
    with telegram_session() as client:
        client.send_message('me', 'note 1')
        client.send_message('me', 'note 2')

    # Read recent messages from a chat
    from telegram_helpers import read_messages
    msgs = read_messages('me', limit=10)  # 'me' = Saved Messages
    for sender, text, when in msgs:
        print(f"{when} {sender}: {text}")

Setup checklist (one-time):
  1. Get api_id + api_hash from https://my.telegram.org/apps
  2. python3 /sandbox/session.py --setup-credentials API_ID API_HASH +PHONE
  3. python3 /sandbox/session.py --bootstrap
  4. Verify: python3 /sandbox/session.py --check  → valid: true
"""
import json
import os
from contextlib import contextmanager
from datetime import datetime
from typing import Generator, Optional

try:
    from paths import telegram_credentials as _credentials_path
    from paths import telegram_session as _session_path
except ImportError:
    _credentials_path = None
    _session_path = None


def load_credentials() -> dict:
    """Load api_id + api_hash + phone from credentials file.

    Raises RuntimeError if file is missing. Call session.py --setup-credentials
    first to populate it.
    """
    p = _credentials_path()
    if p is None or not p.exists():
        raise RuntimeError(
            f"No Telegram credentials at {p}. "
            f"Run: python3 /sandbox/session.py --setup-credentials API_ID API_HASH PHONE"
        )
    with open(p) as f:
        data = json.load(f)
    if isinstance(data.get("api_id"), str):
        data["api_id"] = int(data["api_id"])
    return data


def has_valid_session() -> bool:
    """Quick file-presence check. Doesn't call the Telegram API.

    For a real liveness check, use session.py --check (calls get_me).
    """
    sp = _session_path()
    cp = _credentials_path()
    return (
        sp is not None
        and sp.exists()
        and sp.stat().st_size > 0
        and cp is not None
        and cp.exists()
    )


@contextmanager
def telegram_session():
    """Yield a connected, authorized TelegramClient. Disconnects on exit.

    Uses Telethon's sync mode (auto-installed on `from telethon.sync`).
    Caller can call client.send_message, client.get_dialogs, etc. directly.

    Raises RuntimeError if session is missing or invalid.

    Example:
        with telegram_session() as client:
            me = client.get_me()
            print(f"Logged in as {me.first_name}")
            client.send_message('me', 'hello from script')
    """
    try:
        from telethon import TelegramClient
        import telethon.sync  # noqa: F401 — enables sync wrappers
    except ImportError as e:
        raise RuntimeError(
            "telethon not installed. Add 'telethon' to pux.yaml pip_packages."
        ) from e

    if not has_valid_session():
        sp = _session_path()
        cp = _credentials_path()
        raise RuntimeError(
            f"Session not ready. Files missing:\n"
            f"  credentials: {cp} ({'ok' if cp and cp.exists() else 'MISSING'})\n"
            f"  session:     {sp} ({'ok' if sp and sp.exists() else 'MISSING'})\n"
            f"Run: python3 /sandbox/session.py --bootstrap"
        )

    creds = load_credentials()
    client = TelegramClient(str(_session_path()), creds["api_id"], creds["api_hash"])
    client.connect()
    try:
        if not client.is_user_authorized():
            raise RuntimeError(
                "Session file exists but Telegram no longer recognizes it. "
                "Re-run: python3 /sandbox/session.py --bootstrap"
            )
        yield client
    finally:
        client.disconnect()


def post_to_saved_messages(text: str) -> dict:
    """Post a message to your own Saved Messages chat.

    This is the killer feature — use Saved Messages as a note-taking surface.
    One-liner from any agent-written script.

    Args:
        text: Message body. Can be multi-line. Markdown supported (Telegram
              parses **bold**, *italic*, `code`, [links](url)).

    Returns:
        Dict with message_id, sent_at timestamp. Empty on failure.

    Example:
        post_to_saved_messages("remember to buy milk")
        post_to_saved_messages("**CHAPTER 1 NOTES**\\nKey idea: ...")
    """
    with telegram_session() as client:
        msg = client.send_message("me", text)
        return {
            "ok": True,
            "message_id": msg.id,
            "sent_at": datetime.now().isoformat(),
            "char_count": len(text),
        }


def send_message(chat: str, text: str) -> dict:
    """Send a message to any chat (user, group, channel, or Saved Messages).

    Args:
        chat: Username (@example), phone number (+1555...), chat ID (int),
              or 'me' for Saved Messages.
        text: Message body.

    Returns:
        Dict with message_id, target, sent_at. Errors go in 'error' key.

    Example:
        send_message("@john", "hi from the agent")
        send_message("me", "private note")  # same as post_to_saved_messages
    """
    with telegram_session() as client:
        msg = client.send_message(chat, text)
        return {
            "ok": True,
            "message_id": msg.id,
            "target": chat,
            "sent_at": datetime.now().isoformat(),
        }


def read_messages(chat: str, limit: int = 20) -> list[dict]:
    """Read recent messages from a chat. Most recent first.

    Args:
        chat: Username, phone, chat ID, or 'me' for Saved Messages.
        limit: Max messages to return (default 20). Hard cap 100.

    Returns:
        List of dicts with keys: sender (str), text (str), date (ISO str),
        message_id (int). Sender is the display name (first_name + last_name
        or username) — NOT the phone number.

    Example:
        msgs = read_messages('me', limit=5)
        for m in msgs:
            print(f"{m['date']} {m['sender']}: {m['text']}")
    """
    limit = max(1, min(int(limit), 100))
    with telegram_session() as client:
        out = []
        for msg in client.iter_messages(chat, limit=limit):
            sender_name = _sender_display_name(msg.sender)
            out.append({
                "message_id": msg.id,
                "sender": sender_name,
                "text": msg.text or "",  # None if media-only
                "date": msg.date.isoformat() if msg.date else None,
            })
        return out


def list_chats(limit: int = 20) -> list[dict]:
    """List recent chats (conversations). Most recent first.

    Args:
        limit: Max chats to return (default 20). Hard cap 100.

    Returns:
        List of dicts with keys: name (str), chat_id (int), username (str|None),
        last_message_date (ISO str), unread_count (int).

    Example:
        chats = list_chats(limit=10)
        for c in chats:
            print(f"{c['name']} — {c['unread_count']} unread")
    """
    limit = max(1, min(int(limit), 100))
    with telegram_session() as client:
        out = []
        for d in client.iter_dialogs(limit=limit):
            out.append({
                "name": d.name or "(unnamed)",
                "chat_id": d.id,
                "username": getattr(d.entity, "username", None),
                "last_message_date": d.date.isoformat() if d.date else None,
                "unread_count": d.unread_count,
            })
        return out


def search_messages(query: str, chat: Optional[str] = None, limit: int = 20) -> list[dict]:
    """Search messages by keyword. Optional chat filter.

    Args:
        query: Search string (case-insensitive).
        chat: Optional chat to restrict search to (username, ID, or 'me').
        limit: Max results (default 20). Hard cap 100.

    Returns:
        List of dicts with keys: sender, text, date, chat_name, message_id.

    Example:
        results = search_messages("meeting notes", chat="me")
    """
    limit = max(1, min(int(limit), 100))
    with telegram_session() as client:
        kwargs = {"limit": limit, "search": query}
        if chat is not None:
            kwargs["entity"] = chat
        out = []
        for msg in client.iter_messages(**kwargs):
            out.append({
                "message_id": msg.id,
                "sender": _sender_display_name(msg.sender),
                "text": msg.text or "",
                "date": msg.date.isoformat() if msg.date else None,
                "chat_id": getattr(msg, "peer_id", None),
            })
        return out


def _sender_display_name(sender) -> str:
    """Best-effort display name from a Telethon User/Chat object."""
    if sender is None:
        return "(unknown)"
    # Try User fields first
    first = getattr(sender, "first_name", None)
    last = getattr(sender, "last_name", None)
    if first or last:
        parts = [p for p in [first, last] if p]
        return " ".join(parts)
    # Fall back to chat title
    title = getattr(sender, "title", None)
    if title:
        return title
    # Fall back to username
    username = getattr(sender, "username", None)
    if username:
        return f"@{username}"
    return f"id:{getattr(sender, 'id', '?')}"
