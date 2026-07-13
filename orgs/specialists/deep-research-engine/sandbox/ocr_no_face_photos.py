#!/usr/bin/env python3
"""OCR + caption the photos that have NO detected face.

These are text screenshots, scenes, objects, documents — NOT face entities.
Surface them as TEXT (OCR) + a brief caption so the user can browse.

Output: entities/text_and_scenes/
  - info.md  (all no-face photos + their OCR text + caption)
  - ocr_cache.json (raw OCR/caption per photo, for resume)
  - photos/   (the source JPGs)

Runs against the host media-mcp (MEDIA_MCP_URL=http://localhost:8101/mcp).
Works from inside the sandbox too — set MEDIA_MCP_URL accordingly.
"""
import ast
import base64
import json
import os
import shutil
import urllib.request
from pathlib import Path

MEDIA_URL = os.environ.get("MEDIA_MCP_URL", "http://localhost:8101/mcp")
PHOTOS_DIR = Path(os.environ.get("PHOTOS_DIR",
    "data/telegram-dump/Raw_ChatExport_2026-03-13/photos"))
RUN_DIR = Path(os.environ.get("RUN_DIR",
    "orgs/specialists/deep-research-engine/artifacts/run-2026-07-12"))
OUT_DIR = RUN_DIR / "entities" / "text_and_scenes"
_rpc_id = 0


def _rpc(tool, args=None):
    global _rpc_id
    _rpc_id += 1
    body = json.dumps({"jsonrpc": "2.0", "id": _rpc_id, "method": "tools/call",
                       "params": {"name": tool, "arguments": args or {}}})
    req = urllib.request.Request(MEDIA_URL, data=body.encode(),
        headers={"Content-Type": "application/json",
                 "Accept": "application/json, text/event-stream"})
    with urllib.request.urlopen(req, timeout=120) as r:
        raw = r.read().decode()
    for line in raw.splitlines():
        line = line.strip()
        if line.startswith("data:"):
            line = line[5:].strip()
        if line.startswith("{"):
            d = json.loads(line)
            if "error" in d:
                return {"error": str(d["error"])}
            result = d.get("result", {})
            sc = result.get("structuredContent", {})
            if sc:
                return sc
            for c in result.get("content", []):
                if c.get("type") == "text":
                    try:
                        return json.loads(c["text"])
                    except Exception:
                        return {"text": c["text"]}
            return result
    return {"error": "no JSON in response"}


def _extract_text(resp, key_hint):
    """media-mcp returns results as a stringified dict like "{'<OCR>': 'text'}"."""
    if not resp:
        return ""
    # structuredContent may have results as a string
    val = resp.get("results") or resp.get("result") or resp.get(key_hint) or ""
    if isinstance(val, dict):
        # Take any value
        return " ".join(str(v) for v in val.values()).strip()
    if isinstance(val, list):
        return " ".join(str(x) for x in val).strip()
    val = str(val).strip()
    # Parse stringified dict "{'<OCR>': '...'}"
    if val.startswith("{") and val.endswith("}"):
        try:
            parsed = ast.literal_eval(val)
            if isinstance(parsed, dict):
                return " ".join(str(v) for v in parsed.values()).strip()
        except Exception:
            pass
    return val


def analyze_photo(path):
    """Upload + OCR + caption via Florence-2. media-mcp fetches the URL itself."""
    data = base64.b64encode(path.read_bytes()).decode()
    up = _rpc("upload", {"data": data, "mime_type": "image/jpeg"})
    url = up.get("url") or up.get("uri") or up.get("location")
    if not url:
        return {"ocr": "", "caption": "", "error": "upload failed"}
    r = _rpc("analyze_image", {"imageSource": url, "prompt": " ", "task": "ocr"})
    ocr_text = _extract_text(r, "<OCR>")
    r2 = _rpc("analyze_image", {"imageSource": url, "prompt": " ", "task": "detailed_caption"})
    caption = _extract_text(r2, "<DETAILED_CAPTION>")
    return {"ocr": ocr_text, "caption": caption}


def main():
    fa = json.loads((RUN_DIR / "face_analysis.json").read_text())
    face_photos = {e["photo"] for e in fa if e.get("n_faces", 0) > 0}
    no_face = [e["photo"] for e in fa if e.get("n_faces", 0) == 0]
    print(f"face photos: {len(face_photos)}, no-face photos: {len(no_face)}")

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    (OUT_DIR / "photos").mkdir(exist_ok=True)

    cache_path = OUT_DIR / "ocr_cache.json"
    cache = json.loads(cache_path.read_text()) if cache_path.exists() else {}

    results = []
    for i, photo in enumerate(no_face):
        if photo in cache and "error" not in cache[photo].get("ocr", ""):
            results.append({"photo": photo, **cache[photo]})
            continue
        src = PHOTOS_DIR / photo
        if not src.exists():
            continue
        try:
            r = analyze_photo(src)
        except Exception as e:
            r = {"ocr": "", "caption": "", "error": str(e)[:80]}
        cache[photo] = r
        results.append({"photo": photo, **r})
        if (i + 1) % 10 == 0:
            cache_path.write_text(json.dumps(cache, indent=2))
            print(f"  [{i+1}/{len(no_face)}] checkpoint ({sum(1 for v in cache.values() if len(v.get('ocr',''))>50)} text)", flush=True)

    cache_path.write_text(json.dumps(cache, indent=2))

    text_shots = [r for r in results if len(r.get("ocr", "")) > 50 and not r.get("error")]
    scenes = [r for r in results if len(r.get("ocr", "")) <= 50 or r.get("error")]

    lines = ["# Photos without faces — text screenshots + scenes", "",
             f"**Total:** {len(results)} photos (no face detected)", "",
             "These are NOT face entities. They are either text screenshots (surfaced as OCR text below) or scene/object photos.", "",
             f"## Text screenshots ({len(text_shots)})", ""]
    for r in text_shots:
        src = PHOTOS_DIR / r["photo"]
        dst = OUT_DIR / "photos" / r["photo"]
        if src.exists() and not dst.exists():
            shutil.copy2(src, dst)
        lines += [f"### `{r['photo']}`", "",
                  f"**Caption:** {r.get('caption','')}", "",
                  "**OCR text:**", "```", r.get("ocr", "")[:2000], "```", ""]
    lines += ["", f"## Scene / object photos ({len(scenes)})", ""]
    for r in scenes:
        src = PHOTOS_DIR / r["photo"]
        dst = OUT_DIR / "photos" / r["photo"]
        if src.exists() and not dst.exists():
            shutil.copy2(src, dst)
        lines += [f"- `{r['photo']}` — {r.get('caption','(no caption)')}"]
    (OUT_DIR / "info.md").write_text("\n".join(lines))
    print(f"wrote {OUT_DIR}/info.md — {len(text_shots)} text screenshots, {len(scenes)} scenes")


if __name__ == "__main__":
    main()
