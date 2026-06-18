# INGEST_TELEGRAM_EXPORT

Parse a Telegram chat export into a normalized item list ready for downstream ingestion (audio diarization, image analysis, entity extraction). This is **step 1 of the ingestion pipeline**. Every other ingestion skill expects items in the schema produced here.

## When to use

You receive a Telegram export directory. It contains `messages.html` (HTML export) or `result.json` (JSON export), plus media subdirectories (`photos/`, `voice_messages/`, `video_files/`, `stickers/`).

Run this skill FIRST, before any media processing or entity extraction.

## Tool

`sandbox/telegram_parser.py` — standalone CLI, no dependencies beyond Python stdlib + beautifulsoup4 + lxml (both installed in the sandbox image).

```bash
# Parse export → write items JSON to disk
python3 sandbox/telegram_parser.py parse \
    --input data/ChatExport_<date>/ \
    --output /sandbox/workspace/parsed_items.json

# Re-run stats on an existing items JSON
python3 sandbox/telegram_parser.py stats --input /sandbox/workspace/parsed_items.json
```

`--format json|html` is optional. If omitted, auto-detects (`result.json` → JSON, else `messages.html` → HTML).

## Output schema

```json
{
  "source": "html",
  "stats": {
    "total": 449,
    "media": 436,
    "senders": ["Will", "Eren G", "..."],
    "date_range": {"earliest": "ISO", "latest": "ISO"}
  },
  "orphan_media_count": 85,
  "items": [
    {
      "type": "message|voice|video|photo|sticker",
      "path": "photos/photo_1@13-03-2026_08-59-37.jpg | null",
      "text": "message text | null",
      "timestamp": "2026-03-13T08:59:37-05:00",
      "sender": "Will | Unknown",
      "forwarded": false
    }
  ]
}
```

The top-level wrapper is metadata. The `items` array is what downstream skills consume.

## Per-type routing

After parsing, route each item by `type`:

| `type` | Downstream skill | What happens there |
|---|---|---|
| `message` | `INGEST_ENTITY_EXTRACTION` | Text → LLM extract persons/orgs/topics/dates → SurrealDB |
| `voice` | `INGEST_AUDIO_DIARIZATION` | `.ogg` → Parakeet transcript + Pyannote speakers → SurrealDB `transcript` table |
| `video` | `INGEST_AUDIO_DIARIZATION` + `INGEST_IMAGE_ANALYSIS` | Extract audio track → diarize. Sample keyframes → analyze. |
| `photo` | `INGEST_IMAGE_ANALYSIS` | Image → Florence-2 caption + YOLO objects + WD14 tags → SurrealDB `media` table |
| `sticker` | (usually skipped) | Stickers carry no entity information. Skip unless the user asks for them. |

The `text` field on media items is usually `null`. Voice/video get their "text" populated by the diarization skill (as transcript). Photos get description text from image analysis. **An item with `type=voice` and empty `text` after diarization is an auditor failure** (see `AUDIT_INGESTION_COVERAGE`).

## Orphan media

The parser scans `photos/`, `voice_messages/`, `video_files/`, `stickers/` for files not referenced by any message. These appear in the items list with `timestamp="Found in archive"` and `sender="Unknown"`. They're still real media — process them through the same downstream pipeline.

Thumbnails (`*_thumb.*`) are filtered out — never ingest those.

## Behaviors already handled (do not re-implement)

- **Sender name timestamp pollution**: Telegram HTML exports append "  DD.MM.YYYY HH:MM:SS" to sender names. The parser strips this via regex. You will see clean names like `"Will"`, not `"Will  13.03.2026 08:59:37"`. The historical silent failure — person record created with the timestamp as part of the name — cannot recur as long as you consume `sender` from parser output rather than scraping the HTML yourself.
- **Forwarded flag**: Set on any message where Telegram marked the original sender. The original sender's name is hidden — these items will have `sender="Unknown"`. Do not treat "Unknown" as a literal person; treat it as "forwarded, original sender not in export."
- **Timestamp normalization**: All three input formats (Telegram HTML `DD.MM.YYYY HH:MM:SS UTC-X`, ISO `YYYY-MM-DDTHH:MM:SS±HH:MM`, Unix epoch) are normalized to ISO 8601 with offset.
- **Service messages** (join/leave/pin): Automatically skipped.

## Common pitfalls

1. **Output goes to stdout by default** — that's the items array only (no wrapper metadata). If you want stats too, use `--output <file>` and read the file. The wrapper object is what gets written to disk; stdout gets the bare array.
2. **JSON exports can be very large** — `result.json` from a multi-year chat can be hundreds of MB. The parser holds the whole thing in memory. If you hit OOM, ask the user to export a smaller date range.
3. **Voice files are `.ogg` (Opus)** — most media tools need conversion to `.wav` first. The audio diarization skill handles this.
4. **Photos can have unicode filenames** — always pass paths as bytes-safe strings. The parser uses `pathlib.Path.iterdir()` which handles this; your downstream code should too.

## Smoke test

Verify the parser works on a fresh export:

```bash
python3 sandbox/telegram_parser.py parse \
    --input data/ChatExport_2026-03-13/ \
    --output /tmp/test_parse.json

# Expected: 449 items, 436 media, 4 senders, 85 orphan media
# Type breakdown (verified 2026-03-13):
#   photo: 389, voice: 34, message: 13, video: 13
```

If item counts are 0, check:
- The input dir actually contains `messages.html` or `result.json`
- `beautifulsoup4` and `lxml` are installed (`pip install beautifulsoup4 lxml`)
- The HTML isn't a "loading…" stub (Telegram Desktop sometimes exports incompletely — re-export)

## Idempotency

Parsing is fully deterministic — re-running on the same input produces byte-identical output. Safe to re-run as part of a re-ingest pipeline.

## Storage hook (after Feature 6 / entity extraction)

When you create a `source` record in SurrealDB for the export, use this shape:

```bash
python3 sandbox/surreal_client.py insert --table source --input '{
  "path": "data/ChatExport_2026-03-13/",
  "type": "telegram_export",
  "format": "html",
  "parsed_at": "<ISO timestamp>",
  "item_count": 449
}'
```

Then every downstream record (person, media, transcript) created from this export gets an `extracted_from` edge back to this source. This is how the auditor traces any record back to its origin.
