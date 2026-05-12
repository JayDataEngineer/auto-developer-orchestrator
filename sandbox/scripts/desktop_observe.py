#!/usr/bin/env python3
"""Desktop observer: screenshot + OCR + window list in one call.

Runs inside the sandbox via ExecInSandbox. Outputs JSON to stdout with:
  image_b64: base64-encoded PNG screenshot
  elements:  OCR-detected text regions with bounding boxes and center coords
  windows:   window list from wmctrl
  resolution: display geometry
  ocr_available: whether tesseract was available

Graceful degradation: if tesseract is missing, returns empty elements
and ocr_available=false. Screenshot + windows always work.
"""
import subprocess
import json
import sys
import os
import base64

SCREENSHOT_PATH = "/tmp/desktop_observe.png"
TESSERACT_OUTPUT = "/tmp/desktop_ocr"
MAX_ELEMENTS = 50
OCR_CONFIDENCE_THRESHOLD = 30
WORD_GROUP_GAP = 50  # pixels — merge adjacent words within this gap


def run(cmd, **kwargs):
    """Run a command, return CompletedProcess."""
    env = {**os.environ}
    if "DISPLAY" not in env:
        env["DISPLAY"] = ":99"
    return subprocess.run(cmd, capture_output=True, text=True, env=env, **kwargs)


def take_screenshot(display):
    """Screenshot fallback chain: import -> scrot -> xwd."""
    env = {**os.environ, "DISPLAY": display}
    cmds = [
        ["import", "-window", "root", SCREENSHOT_PATH],
        ["scrot", SCREENSHOT_PATH],
    ]
    for cmd in cmds:
        r = subprocess.run(cmd, capture_output=True, env=env)
        if r.returncode == 0 and os.path.exists(SCREENSHOT_PATH) and os.path.getsize(SCREENSHOT_PATH) > 100:
            return True
    # Fallback: xwd + convert
    r = subprocess.run(
        ["bash", "-c",
         f"xwd -root -out /tmp/x11-obs.xwd && convert xwd:/tmp/x11-obs.xwd {SCREENSHOT_PATH}"],
        capture_output=True, env=env,
    )
    return r.returncode == 0 and os.path.exists(SCREENSHOT_PATH)


def run_ocr(image_path):
    """Run tesseract OCR, return parsed elements. Returns [] if tesseract missing."""
    r = run(["which", "tesseract"])
    if r.returncode != 0:
        return []

    r = run([
        "tesseract", image_path, TESSERACT_OUTPUT,
        "-l", "eng", "--psm", "3",
        "-c", "tessedit_create_tsv=1",
    ])
    if r.returncode != 0:
        return []

    tsv_path = TESSERACT_OUTPUT + ".tsv"
    if not os.path.exists(tsv_path):
        return []

    with open(tsv_path, "r") as f:
        tsv_text = f.read()

    return parse_tesseract_tsv(tsv_text)


def parse_tesseract_tsv(tsv_text):
    """Parse tesseract TSV into grouped elements.

    Tesseract TSV columns (tab-separated):
    level  page_num  block_num  par_num  line_num  word_num
    left   top       width      height   conf      text

    We keep level=5 (word) rows with conf >= threshold, then group
    adjacent words on the same line.
    """
    lines = tsv_text.strip().split("\n")
    if len(lines) < 2:
        return []

    header = lines[0].split("\t")
    idx = {}
    for i, h in enumerate(header):
        idx[h.strip()] = i

    for col in ("level", "left", "top", "width", "height", "conf", "text"):
        if col not in idx:
            return []

    words = []
    for line in lines[1:]:
        parts = line.split("\t")
        if len(parts) <= idx["text"]:
            continue
        try:
            level = int(parts[idx["level"]])
            if level != 5:
                continue
            conf = float(parts[idx["conf"]])
            if conf < OCR_CONFIDENCE_THRESHOLD:
                continue
            text = parts[idx["text"]].strip()
            if not text:
                continue
            left = int(parts[idx["left"]])
            top = int(parts[idx["top"]])
            w = int(parts[idx["width"]])
            h = int(parts[idx["height"]])
            words.append({
                "text": text,
                "left": left, "top": top,
                "width": w, "height": h,
                "right": left + w,
                "bottom": top + h,
            })
        except (ValueError, IndexError):
            continue

    return group_words(words)


def group_words(words):
    """Group adjacent words into labeled elements.

    Sort by (top, left). Merge words on the same line (vertical overlap)
    with horizontal gap < 50px. Cap at MAX_ELEMENTS.
    """
    if not words:
        return []

    words.sort(key=lambda w: (w["top"], w["left"]))

    groups = []
    current = [words[0]]

    for w in words[1:]:
        prev = current[-1]
        # Same line: vertical overlap
        vertical_overlap = w["top"] < prev["bottom"] and w["bottom"] > prev["top"]
        horizontal_gap = w["left"] - prev["right"]

        if vertical_overlap and horizontal_gap < WORD_GROUP_GAP:
            current.append(w)
        else:
            groups.append(merge_group(current))
            current = [w]
    groups.append(merge_group(current))

    # Cap: keep largest (most visible) elements
    if len(groups) > MAX_ELEMENTS:
        groups.sort(key=lambda g: g["w"] * g["h"], reverse=True)
        groups = groups[:MAX_ELEMENTS]
        groups.sort(key=lambda g: (g["y"], g["x"]))

    # Assign sequential IDs
    for i, g in enumerate(groups, 1):
        g["id"] = i

    return groups


def merge_group(words):
    """Merge a list of adjacent words into a single element."""
    left = min(w["left"] for w in words)
    top = min(w["top"] for w in words)
    right = max(w["right"] for w in words)
    bottom = max(w["bottom"] for w in words)
    text = " ".join(w["text"] for w in words)

    return {
        "text": text[:80],
        "x": left, "y": top,
        "w": right - left, "h": bottom - top,
        "cx": left + (right - left) // 2,
        "cy": top + (bottom - top) // 2,
    }


def get_windows(display):
    """Get window list via wmctrl."""
    r = run(["wmctrl", "-l"], env={**os.environ, "DISPLAY": display})
    if r.returncode != 0:
        return []

    windows = []
    for line in r.stdout.strip().split("\n"):
        if not line:
            continue
        parts = line.split(None, 3)
        if len(parts) < 4:
            continue
        windows.append({
            "id": parts[0],
            "name": parts[3],
        })
    return windows


def get_resolution(display):
    """Get display geometry via xdotool."""
    r = run(["xdotool", "getdisplaygeometry"], env={**os.environ, "DISPLAY": display})
    if r.returncode != 0:
        return {"width": 1280, "height": 720}
    parts = r.stdout.strip().split()
    if len(parts) < 2:
        return {"width": 1280, "height": 720}
    return {"width": int(parts[0]), "height": int(parts[1])}


def main():
    display = os.environ.get("DISPLAY", ":99")

    if not take_screenshot(display):
        json.dump({"error": "screenshot failed"}, sys.stdout)
        sys.exit(1)

    with open(SCREENSHOT_PATH, "rb") as f:
        image_b64 = base64.b64encode(f.read()).decode("ascii")

    elements = run_ocr(SCREENSHOT_PATH)
    windows = get_windows(display)
    resolution = get_resolution(display)

    result = {
        "image_b64": image_b64,
        "elements": elements,
        "windows": windows,
        "resolution": resolution,
        "ocr_available": len(elements) > 0,
    }
    json.dump(result, sys.stdout)


if __name__ == "__main__":
    main()
