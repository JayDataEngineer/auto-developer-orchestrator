# Telegram Notes & Automation

A personal Telegram agent. Post to Saved Messages, read mentions, send messages to anyone — all from scripts the agent writes on the fly. **Never open the Telegram app for routine tasks again.**

## Mission
Telegram is a note-taking surface, a messaging surface, and a reading surface. This org automates all three via Telethon (MTProto client) and agent-written Python helpers.

## Key insight
Unlike Twitter (which uses browser cookies we pull via yt-dlp pattern), Telegram uses MTProto — a custom protocol. We can't read cookies from a host browser. Instead we do **one interactive bootstrap** (phone + SMS code, ~30 seconds), then Telethon persists a `.session` SQLite file that's reused forever. After that one-time auth, every subsequent operation is fully automated.

## Capability map
1. **Saved Messages as a clipboard** — `post_to_saved_messages("remember this")` is one line
2. **Read mentions / unread** — pull new messages from any chat or channel
3. **Send to anyone** — `send_message(username, text)` mirrors DMing a person
4. **Search history** — find old messages by keyword

## Self-evolving substrate
The agent builds its own scripts via `make_script` / `run_script`. Once `telegram_helpers` is importable, the agent writes `post_note.py`, `read_mentions.py`, `summarize_unread.py` etc. on the fly. New behavior = new script. Zero Go code.

## Honesty rules
1. **Never assume session validity.** Check with `session.py --check` before messaging ops.
2. **Never log message contents to stdout.** Return only counts + senders (privacy).
3. **One-time bootstrap.** If session dies, escalate to user — don't try to re-auth silently.
