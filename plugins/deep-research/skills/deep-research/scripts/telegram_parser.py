#!/usr/bin/env python3
"""Telegram export parser (standalone CLI).

Standalone CLI — parses Telegram JSON and HTML exports into unified items.
No DRE engine dependencies.

Usage:
    python3 telegram_parser.py parse --input /path/to/telegram/export
    python3 telegram_parser.py parse --input /path/to/export --format json
    python3 telegram_parser.py parse --input /path/to/export --output items.json

Supports two Telegram export formats:
  1. result.json — Structured JSON with message objects
  2. messages.html — HTML export (requires beautifulsoup4 + lxml)

Output schema (each item):
{
    "type": "message" | "voice" | "video" | "photo" | "sticker" | "document",
    "path": str,           # Media file path relative to export dir
    "text": str | None,    # Message text content
    "timestamp": str,      # ISO timestamp
    "sender": str,         # Message sender
    "forwarded": bool      # Whether message was forwarded
}
"""

import argparse
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path


def normalize_timestamp(timestamp_str):
    """Normalize timestamp to ISO format.

    Handles Telegram formats:
    - "13.03.2026 08:59:37 UTC-05:00"
    - "2026-03-13T08:59:37-05:00"
    - Unix timestamp (int/float)
    """
    if not timestamp_str:
        return {"iso": "", "tz_offset": ""}

    ts = str(timestamp_str).strip()

    # Try Telegram HTML format: "13.03.2026 08:59:37 UTC-05:00"
    try:
        cleaned = re.sub(r"UTC([+-])", r"\1", ts)
        dt = datetime.strptime(cleaned, "%d.%m.%Y %H:%M:%S %z")
        return {"iso": dt.isoformat(), "tz_offset": format_tz_offset(dt)}
    except ValueError:
        pass

    # Try ISO format
    try:
        dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return {"iso": dt.isoformat(), "tz_offset": format_tz_offset(dt)}
    except ValueError:
        pass

    # Try Unix timestamp
    try:
        ts_num = float(ts)
        dt = datetime.fromtimestamp(ts_num, tz=timezone.utc)
        return {"iso": dt.isoformat(), "tz_offset": "+00:00"}
    except (ValueError, OSError):
        pass

    return {"iso": ts, "tz_offset": ""}


def format_tz_offset(dt):
    """Format timezone offset as +HH:MM."""
    offset = dt.strftime("%z")
    if offset:
        return f"{offset[:3]}:{offset[3:]}"
    return ""


def extract_json_text(text):
    """Extract text from Telegram JSON message text field.

    Telegram text is either a plain string or a list of mixed
    text/inline entities: [{"text": "hello"}, " world"]
    """
    if isinstance(text, str):
        return text
    if isinstance(text, list):
        return "".join(
            t.get("text", "") if isinstance(t, dict) else str(t)
            for t in text
        )
    return str(text) if text else ""


def parse_json(input_dir):
    """Parse Telegram JSON export (result.json)."""
    result_json = Path(input_dir) / "result.json"
    if not result_json.exists():
        return None, "result.json not found"

    items = []
    try:
        data = json.loads(result_json.read_text(encoding="utf-8"))
    except Exception as e:
        return None, f"Failed to read result.json: {e}"

    messages = data.get("messages", [])
    for msg in messages:
        if msg.get("type") == "service":
            continue

        ts = normalize_timestamp(msg.get("date", ""))
        sender = msg.get("from", "Unknown")
        text = extract_json_text(msg.get("text", ""))
        media_path = msg.get("file", "") or msg.get("photo", "")
        forwarded = bool(msg.get("forwarded_from"))

        # Determine item type
        item_type = msg.get("type", "message")
        if msg.get("media_type") == "voice_message":
            item_type = "voice"
        elif msg.get("media_type") == "video_file":
            item_type = "video"
        elif msg.get("photo"):
            item_type = "photo"
        elif msg.get("sticker_emoji"):
            if not text:
                text = f"[Sticker: {msg['sticker_emoji']}]"
            item_type = "sticker"

        if not text and not media_path:
            continue

        items.append({
            "type": item_type,
            "path": media_path,
            "text": text,
            "timestamp": ts["iso"],
            "sender": sender,
            "forwarded": forwarded,
        })

    return items, None


def parse_html(input_dir):
    """Parse Telegram HTML export (messages.html)."""
    messages_html = Path(input_dir) / "messages.html"
    if not messages_html.exists():
        return None, "messages.html not found"

    try:
        from bs4 import BeautifulSoup
    except ImportError:
        return None, "beautifulsoup4 not installed. Run: pip install beautifulsoup4 lxml"

    items = []
    try:
        html = messages_html.read_text(encoding="utf-8")
        soup = BeautifulSoup(html, "lxml")
    except Exception as e:
        return None, f"Failed to parse HTML: {e}"

    message_divs = soup.find_all("div", class_="message")

    sender = "Unknown"  # sticky across consecutive messages in a sender group

    for div in message_divs:
        if "service" in (div.get("class") or []):
            continue

        # Timestamp
        timestamp_raw = ""
        time_elem = div.find("div", class_="pull_right") or div.find("time")
        if time_elem:
            timestamp_raw = time_elem.get("title", "") or time_elem.text.strip()
        ts = normalize_timestamp(timestamp_raw)

        # Sender — Telegram HTML groups consecutive messages from the same sender
        # under a single from_name on the first message of the group. Subsequent
        # messages in the group have no from_name and must inherit the previous
        # sender. (The "forwarded" div appears on most messages in this archive
        # because the whole chat was forwarded as a unit — it is NOT a per-message
        # sender-change signal, so we ignore it for sender inheritance.)
        from_elem = div.find("div", class_="from_name")
        if from_elem:
            sender = from_elem.text.strip()
            # Telegram HTML appends "  DD.MM.YYYY HH:MM:SS" to sender names
            sender = re.sub(r"\s+\d{2}\.\d{2}\.\d{4}\s+\d{2}:\d{2}:\d{2}$", "", sender)
        # else: inherit previous sender (sticky from_name)

        # Text
        text = None
        text_elem = div.find("div", class_="text")
        if text_elem:
            text = text_elem.text.strip()

        # Media and type
        media_path = None
        item_type = "message"
        forwarded = bool(div.find("div", class_="forwarded"))

        photo_link = div.find("a", class_="photo_wrap")
        if photo_link:
            media_path = photo_link.get("href", "")
            item_type = "photo"

        voice_link = div.find("a", class_="media_voice_message")
        if voice_link:
            media_path = voice_link.get("href", "")
            item_type = "voice"

        video_link = div.find("a", class_="video_file_wrap")
        if video_link:
            video_src = video_link.get("href", "")
            if video_src:
                media_path = video_src
            item_type = "video"
        else:
            video_elem = div.find("div", class_="video")
            if video_elem:
                video_src = video_elem.find("source")
                if video_src:
                    media_path = video_src.get("src", "")
                item_type = "video"

        # Stickers
        is_sticker = bool(
            div.find("img", class_="sticker") or div.find("div", class_="sticker")
        )
        if is_sticker:
            sticker_img = div.find("img", class_="sticker")
            emoji = ""
            if sticker_img:
                emoji = sticker_img.get("alt", "") or sticker_img.get("title", "")
            if not text:
                text = f"[Sticker: {emoji}]" if emoji else "[Sticker]"
            item_type = "sticker"

        if not text and not media_path:
            continue

        items.append({
            "type": item_type,
            "path": media_path,
            "text": text,
            "timestamp": ts["iso"],
            "sender": sender,
            "forwarded": forwarded,
        })

    return items, None


def find_orphan_media(input_dir, items):
    """Find media files not linked from any message."""
    input_path = Path(input_dir)
    existing_paths = {item["path"] for item in items if item.get("path")}

    media_dirs = {
        "voice_messages": "voice",
        "video_files": "video",
        "photos": "photo",
        "stickers": "sticker",
    }

    orphans = []
    for dir_name, type_name in media_dirs.items():
        media_dir = input_path / dir_name
        if not media_dir.exists():
            continue
        for f in sorted(media_dir.iterdir()):
            if f.is_file() and "_thumb" not in f.name:
                rel_path = f"{dir_name}/{f.name}"
                if rel_path not in existing_paths:
                    orphans.append({
                        "type": type_name,
                        "path": rel_path,
                        "text": None,
                        "timestamp": "Found in archive",
                        "sender": "Unknown",
                        "forwarded": False,
                    })

    return orphans


def build_stats(items):
    """Build summary stats from parsed items."""
    senders = {item["sender"] for item in items if item.get("sender")}
    timestamps = [
        item["timestamp"] for item in items
        if item.get("timestamp") and item["timestamp"] not in ("Unknown", "Found in archive")
    ]
    media_count = sum(
        1 for item in items
        if item.get("path") or item["type"] in ("voice", "video", "photo", "sticker")
    )
    date_range = {}
    if timestamps:
        sorted_ts = sorted(timestamps)
        date_range = {"earliest": sorted_ts[0], "latest": sorted_ts[-1]}

    return {
        "total": len(items),
        "media": media_count,
        "senders": sorted(senders),
        "date_range": date_range,
    }


def cmd_parse(args):
    """Parse a Telegram export directory."""
    input_dir = args.input
    if not Path(input_dir).is_dir():
        print(f"ERROR: {input_dir} is not a directory", file=sys.stderr)
        sys.exit(1)

    fmt = args.format
    source = "unknown"

    # Auto-detect format
    if fmt == "json" or (fmt is None and (Path(input_dir) / "result.json").exists()):
        items, error = parse_json(input_dir)
        source = "json"
    elif fmt == "html" or (fmt is None and (Path(input_dir) / "messages.html").exists()):
        items, error = parse_html(input_dir)
        source = "html"
    else:
        print(f"ERROR: No Telegram export found in {input_dir}", file=sys.stderr)
        sys.exit(1)

    if error:
        print(f"ERROR ({source}): {error}", file=sys.stderr)
        sys.exit(1)

    # Find orphan media
    orphans = find_orphan_media(input_dir, items)
    all_items = items + orphans

    # Build output
    stats = build_stats(all_items)
    output = {
        "source": source,
        "stats": stats,
        "orphan_media_count": len(orphans),
        "items": all_items,
    }

    if args.output:
        Path(args.output).write_text(json.dumps(output, indent=2, ensure_ascii=False))
        print(json.dumps({
            "status": "ok",
            "source": source,
            "items": len(all_items),
            "media": stats["media"],
            "senders": len(stats["senders"]),
            "orphan_media": len(orphans),
            "output": args.output,
        }))
    else:
        # Print items to stdout, stats to stderr
        print(json.dumps(all_items, indent=2, ensure_ascii=False))

    print(
        f"\n{stats['total']} items ({stats['media']} media, "
        f"{len(stats['senders'])} senders, {len(orphans)} orphan media) from {source}",
        file=sys.stderr,
    )


def cmd_stats(args):
    """Quick stats on a parsed items JSON file."""
    items = json.loads(Path(args.input).read_text(encoding="utf-8"))

    # Handle both raw items array and wrapped output
    if isinstance(items, dict) and "items" in items:
        items = items["items"]

    stats = build_stats(items)
    print(json.dumps(stats, indent=2))


def main():
    parser = argparse.ArgumentParser(description="Telegram export parser for Pux sandbox")
    sub = parser.add_subparsers(dest="command")

    p = sub.add_parser("parse", help="Parse a Telegram export directory")
    p.add_argument("--input", required=True, help="Path to Telegram export directory")
    p.add_argument("--output", help="Output JSON file (default: stdout)")
    p.add_argument("--format", choices=["json", "html"], default=None,
                   help="Force format (default: auto-detect)")

    p = sub.add_parser("stats", help="Show stats from parsed items JSON")
    p.add_argument("--input", required=True, help="JSON file with parsed items")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {"parse": cmd_parse, "stats": cmd_stats}
    commands[args.command](args)


if __name__ == "__main__":
    main()
