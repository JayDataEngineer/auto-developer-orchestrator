#!/usr/bin/env python3
"""Scan all photos in the Telegram dataset through mimo-v2.5 for face identification.

For each photo, classifies it as screenshot vs actual photograph, extracts face
descriptions for any people visible, and saves results incrementally. Designed
to be resumable — if interrupted, re-run picks up where it left off.

Usage:
    python3 plugins/deep-research/skills/deep-research/scripts/scan_photos.py [--batch-size 1] [--rate-limit 0.5]

Output:
    artifacts/run-2026-07-23/photo_scan_results.jsonl   (incremental)
    artifacts/run-2026-07-23/photo_scan_report.md        (final summary)
"""
import argparse
import base64
import json
import os
import re
import sys
import time
from pathlib import Path

from openai import OpenAI


def _workspace_root():
    """Layout-robust workspace root: env override, else nearest .git ancestor,
    else cwd (works in-repo under plugins/, in dcode's plugin cache, anywhere)."""
    for var in ("WORKSPACE_ROOT", "CLAUDE_PROJECT_DIR"):
        v = os.environ.get(var)
        if v:
            return Path(v).expanduser()
    for anc in Path(__file__).resolve().parents:
        if (anc / ".git").exists():
            return anc
    return Path.cwd()


# ── Config ──────────────────────────────────────────────────────────────────
REPO = _workspace_root()
PHOTO_DIR = REPO / "data/telegram-dump/ChatExport_2026-03-13/photos"
OUTPUT_JSONL = REPO / "artifacts/run-2026-07-23/photo_scan_results.jsonl"
OUTPUT_REPORT = REPO / "artifacts/run-2026-07-23/photo_scan_report.md"

KNOWN_DESCRIPTIONS = {
    "Grady Kirk": "Adult male, graying beard, glasses, city councillor. Former WLM.",
    "Christopher Semok": "Male, 22-23yo, 5'8\", 120 lbs, from Miami. CPUSA leader.",
    "Scott Ernest": "Male, 45yo, extremely overweight, beard, CSKT heritage. Agoraphobic.",
    "Tyler Dipeppe": "Trans woman (MTF), 26yo, 5'4\", 109 lbs, light brown hair. Former Inland Skins.",
    "Steven Butters": "34yo, trans (born female), lives in RV with chickens and dog. OSINT gatherer.",
    "Will (Axolotl)": "Male, OSINT researcher, WLM Montana. Connecticut phone.",
}

CLASSIFY_PROMPT = """You are a forensic photo analyst. Analyze this image and respond in EXACTLY this JSON format (no other text):

{
  "type": "screenshot" | "photograph" | "meme_graphic" | "document",
  "contains_faces": true/false,
  "num_faces": <integer>,
  "faces": [
    {
      "position": "left" | "center" | "right" | "foreground" | "background",
      "age_range": "estimated age range",
      "gender_presentation": "male/female/androgynous/trans",
      "hair": "color, length, style",
      "facial_hair": "none/beard/mustache/goatee/stubble — color",
      "build": "slim/medium/heavy/very heavy",
      "glasses": true/false,
      "clothing": "notable visible clothing",
      "distinguishing_features": "tattoos, scars, piercings, anything notable",
      "confidence_description": "how clear the face is"
    }
  ],
  "content_summary": "one sentence: what does this image show?",
  "notable_text": "any visible text in the image (short excerpts)"
}

Rules:
- "screenshot" = captures a phone screen, chat app, social media, website
- "photograph" = taken with a camera (selfie, portrait, candid, group photo)
- Only set contains_faces=true if ACTUAL HUMAN FACES are visible (not just tiny avatars in a chat screenshot — if the face is < 50px in a chat screenshot, set contains_faces=false)
- Be precise and factual. No speculation."""


def load_env():
    """Load OPENROUTER_API_KEY from .env."""
    env_path = REPO / ".env"
    if env_path.exists():
        for line in env_path.read_text().splitlines():
            if line.startswith("OPENROUTER_API_KEY="):
                return line.split("=", 1)[1].strip()
    return os.environ.get("OPENROUTER_API_KEY", "")


def get_photo_files():
    """Get all non-thumbnail photos sorted by number."""
    photos = []
    for f in PHOTO_DIR.glob("photo_*.jpg"):
        if "thumb" in f.name:
            continue
        photos.append(f)
    # Sort by photo number
    def extract_num(p):
        try:
            return int(p.name.split("_")[1])
        except (IndexError, ValueError):
            return 0
    photos.sort(key=extract_num)
    return photos


def load_completed():
    """Load already-processed photo names for resumability."""
    completed = set()
    if OUTPUT_JSONL.exists():
        with open(OUTPUT_JSONL) as f:
            for line in f:
                try:
                    rec = json.loads(line)
                    completed.add(rec["photo"])
                except (json.JSONDecodeError, KeyError):
                    pass
    return completed


def scan_photo(client, photo_path, retries=2):
    """Send one photo to mimo-v2.5 and get structured description."""
    with open(photo_path, "rb") as f:
        img_b64 = base64.b64encode(f.read()).decode()

    for attempt in range(retries + 1):
        resp = client.chat.completions.create(
            model="xiaomi/mimo-v2.5",
            messages=[{
                "role": "user",
                "content": [
                    {"type": "text", "text": CLASSIFY_PROMPT},
                    {"type": "image_url", "image_url": {
                        "url": f"data:image/jpeg;base64,{img_b64}"
                    }}
                ]
            }],
            max_tokens=1200,  # increased — mimo-v2.5 uses reasoning tokens
            temperature=0.1,
            # mimo-v2.5 is a reasoning model; "low" keeps it fast without
            # burning tokens on long reasoning chains. Must be low/medium/high.
            extra_body={"reasoning_effort": "low", "provider": {"only": ["Parasail"]}},
        )

        raw = resp.choices[0].message.content or ""

        # Empty response — mimo-v2.5 sometimes burns all tokens on reasoning
        if not raw.strip():
            if attempt < retries:
                time.sleep(0.5)
                continue
            return {
                "raw_response": "",
                "parsed": None,
                "parse_ok": False,
                "parse_error": "empty response (model produced no visible content)",
            }

        result = {"raw_response": raw}

        # Strategy 1: try stripping markdown fences
        json_str = raw.strip()
        if "```json" in json_str:
            json_str = json_str.split("```json")[1].split("```")[0].strip()
        elif "```" in json_str:
            json_str = json_str.split("```")[1].split("```")[0].strip()

        # Strategy 2: try direct parse
        try:
            result["parsed"] = json.loads(json_str)
            result["parse_ok"] = True
            return result
        except json.JSONDecodeError:
            pass

        # Strategy 3: find the outermost { ... } with brace matching
        if "{" in raw:
            start = raw.index("{")
            depth = 0
            end = start
            for i, c in enumerate(raw[start:], start):
                if c == "{":
                    depth += 1
                elif c == "}":
                    depth -= 1
                    if depth == 0:
                        end = i + 1
                        break
            try:
                result["parsed"] = json.loads(raw[start:end])
                result["parse_ok"] = True
                return result
            except json.JSONDecodeError as e:
                result["parse_error"] = f"brace-match failed: {e}"

        # Strategy 4: extract key fields with regex as fallback
        fallback = {}
        for field in ["type", "contains_faces", "num_faces", "content_summary"]:
            m = re.search(rf'"({field})"\s*:\s*([^,}}]+)', raw)
            if m:
                val = m.group(2).strip().strip('"')
                if val in ("true", "false"):
                    val = val == "true"
                elif val.isdigit():
                    val = int(val)
                fallback[field] = val
        if fallback:
            result["parsed"] = fallback
            result["parse_ok"] = True
            result["parse_method"] = "regex_fallback"
            return result

        result["parsed"] = None
        result["parse_ok"] = False
        return result

    return result


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--rate-limit", type=float, default=0.3,
                    help="Seconds between API calls (default: 0.3)")
    ap.add_argument("--limit", type=int, default=0,
                    help="Max photos to process (0 = all)")
    ap.add_argument("--only-faces", action="store_true",
                    help="Skip photos already classified as no-faces")
    args = ap.parse_args()

    api_key = load_env()
    if not api_key:
        print("ERROR: OPENROUTER_API_KEY not found in .env", file=sys.stderr)
        sys.exit(1)

    client = OpenAI(api_key=api_key, base_url="https://openrouter.ai/api/v1")
    photos = get_photo_files()
    completed = load_completed()

    todo = [p for p in photos if p.name not in completed]
    if args.limit > 0:
        todo = todo[:args.limit]

    print(f"Total photos: {len(photos)}")
    print(f"Already done: {len(completed)}")
    print(f"To process:   {len(todo)}")
    print(f"Output:       {OUTPUT_JSONL}")
    print()

    # Open in append mode for incremental writes
    with open(OUTPUT_JSONL, "a") as out:
        for i, photo in enumerate(todo):
            try:
                result = scan_photo(client, photo)
                record = {
                    "photo": photo.name,
                    "size_bytes": photo.stat().st_size,
                    **result,
                }
                out.write(json.dumps(record) + "\n")
                out.flush()

                # Progress
                parsed = result.get("parsed", {}) or {}
                ptype = parsed.get("type", "?")
                nfaces = parsed.get("num_faces", 0)
                summary = parsed.get("content_summary", "")[:60]
                status = "✅" if result.get("parse_ok") else "⚠️"
                print(f"  [{len(completed)+i+1}/{len(photos)}] {status} {photo.name:<45} {ptype:<12} faces={nfaces} {summary}")

            except Exception as e:
                print(f"  [{len(completed)+i+1}/{len(photos)}] ❌ {photo.name:<45} ERROR: {e}", file=sys.stderr)
                # Write error record
                out.write(json.dumps({"photo": photo.name, "error": str(e)}) + "\n")
                out.flush()

            time.sleep(args.rate_limit)

    print(f"\nDone. Results: {OUTPUT_JSONL}")
    print(f"Run `python3 plugins/deep-research/skills/deep-research/scripts/photo_scan_report.py` to generate the summary.")


if __name__ == "__main__":
    main()
