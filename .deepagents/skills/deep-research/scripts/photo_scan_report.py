#!/usr/bin/env python3
"""Generate a markdown identification report from photo_scan_results.jsonl.

Cross-references detected faces against known physical descriptions and
produces a sorted, ranked identification sheet.

Usage:
    python3 .deepagents/skills/deep-research/scripts/photo_scan_report.py
"""
import json
import re
from collections import defaultdict
from pathlib import Path

REPO = Path(__file__).resolve().parents[4]
INPUT = REPO / "artifacts/run-2026-07-23/photo_scan_results.jsonl"
OUTPUT = REPO / "artifacts/run-2026-07-23/photo_scan_report.md"

KNOWN_PROFILES = [
    {
        "name": "Grady Kirk",
        "keywords": ["gray", "grey", "salt", "beard", "council"],
        "desc": "Adult male, graying beard, glasses. Elected city councillor. Former WLM.",
        "age": "30s-40s",
    },
    {
        "name": "Christopher Semok",
        "keywords": ["young", "slim", "thin", "5'8", "120"],
        "desc": "Male, 22-23yo, 5'8\", 120 lbs, from Miami. CPUSA chapter leader.",
        "age": "22-23",
    },
    {
        "name": "Scott Ernest",
        "keywords": ["overweight", "heavy", "45", "beard", "large"],
        "desc": "Male, 45yo, extremely overweight, beard, CSKT (Indigenous).",
        "age": "45",
    },
    {
        "name": "Tyler Dipeppe",
        "keywords": ["trans", "5'4", "109", "slim", "petite", "brown hair"],
        "desc": "Trans woman, 26yo, 5'4\", 109 lbs, light brown shoulder-length hair.",
        "age": "26",
    },
    {
        "name": "Steven Butters",
        "keywords": ["rv", "chicken", "dog", "34", "trans", "born female"],
        "desc": "34yo, trans (born female), lives in RV with animals.",
        "age": "34",
    },
]


def load_results():
    records = []
    if not INPUT.exists():
        print(f"ERROR: {INPUT} not found. Run scan_photos.py first.")
        return records
    with open(INPUT) as f:
        for line in f:
            try:
                rec = json.loads(line)
                records.append(rec)
            except json.JSONDecodeError:
                pass
    return records


def score_match(face_desc, profile):
    """Score how well a face description matches a known profile (0-100)."""
    text = json.dumps(face_desc).lower()
    score = 0
    matches = []
    for kw in profile["keywords"]:
        if kw.lower() in text:
            score += 15
            matches.append(kw)
    # Age proximity
    age_str = str(face_desc.get("age_range", "")).lower()
    if profile["age"] in age_str:
        score += 20
        matches.append(f"age~{profile['age']}")
    return min(score, 100), matches


def main():
    records = load_results()
    if not records:
        return

    # Categorize
    photos_with_faces = []
    screenshots = []
    other = []
    errors = []

    for rec in records:
        if "error" in rec:
            errors.append(rec)
            continue
        parsed = rec.get("parsed")
        if not parsed:
            errors.append(rec)
            continue
        ptype = parsed.get("type", "unknown")
        has_faces = parsed.get("contains_faces", False)

        if has_faces and parsed.get("num_faces", 0) > 0:
            photos_with_faces.append(rec)
        elif ptype == "screenshot":
            screenshots.append(rec)
        else:
            other.append(rec)

    # For face photos, score against known profiles
    identification_candidates = []
    for rec in photos_with_faces:
        parsed = rec["parsed"]
        for face in parsed.get("faces", []):
            for profile in KNOWN_PROFILES:
                score, matches = score_match(face, profile)
                if score > 0:
                    identification_candidates.append({
                        "photo": rec["photo"],
                        "candidate": profile["name"],
                        "score": score,
                        "matches": matches,
                        "face_desc": face,
                        "content": parsed.get("content_summary", ""),
                    })

    # Sort by score descending
    identification_candidates.sort(key=lambda x: -x["score"])

    # Write report
    lines = [
        "# Photo Scan Report — Face Identification",
        f"**Photos scanned:** {len(records)}",
        f"**Photos with faces:** {len(photos_with_faces)}",
        f"**Screenshots:** {len(screenshots)}",
        f"**Other (memes/docs):** {len(other)}",
        f"**Errors:** {len(errors)}",
        "",
        "---",
        "",
        "## Face Photos — All Detected Faces",
        "",
    ]

    for rec in sorted(photos_with_faces, key=lambda r: r["photo"]):
        parsed = rec["parsed"]
        lines.append(f"### {rec['photo']}")
        lines.append(f"**Type:** {parsed.get('type', '?')} | **Faces:** {parsed.get('num_faces', 0)}")
        lines.append(f"**Content:** {parsed.get('content_summary', 'N/A')}")
        if parsed.get("notable_text"):
            lines.append(f"**Text:** {parsed['notable_text'][:100]}")
        for j, face in enumerate(parsed.get("faces", []), 1):
            lines.append(f"\n**Face {j}** ({face.get('position', '?')}):")
            lines.append(f"- Age: {face.get('age_range', '?')}")
            lines.append(f"- Gender: {face.get('gender_presentation', '?')}")
            lines.append(f"- Hair: {face.get('hair', '?')}")
            lines.append(f"- Facial hair: {face.get('facial_hair', '?')}")
            lines.append(f"- Build: {face.get('build', '?')}")
            lines.append(f"- Glasses: {face.get('glasses', '?')}")
            lines.append(f"- Clothing: {face.get('clothing', '?')}")
            lines.append(f"- Distinguishing: {face.get('distinguishing_features', '?')}")
        lines.append("")

    lines.extend([
        "---",
        "",
        "## Identification Candidates — Ranked",
        "",
        "Faces scored against known physical descriptions. Higher score = better match.",
        "",
    ])

    if identification_candidates:
        lines.append("| Score | Photo | Candidate | Matched Signals | Face Description |")
        lines.append("|-------|-------|-----------|-----------------|------------------|")
        for c in identification_candidates:
            face = c["face_desc"]
            desc = f"{face.get('age_range','?')}, {face.get('gender_presentation','?')}, {face.get('hair','?')}, {face.get('facial_hair','?')}, {face.get('build','?')}"
            lines.append(f"| {c['score']} | {c['photo']} | **{c['candidate']}** | {', '.join(c['matches'])} | {desc} |")
    else:
        lines.append("*No candidates scored above threshold.*")

    lines.extend(["", "---", "", "## Screenshots (no faces)", ""])
    for rec in sorted(screenshots, key=lambda r: r["photo"]):
        parsed = rec["parsed"]
        lines.append(f"- **{rec['photo']}**: {parsed.get('content_summary', 'N/A')[:80]}")

    Path(OUTPUT).write_text("\n".join(lines))
    print(f"Report written: {OUTPUT}")
    print(f"  Photos with faces: {len(photos_with_faces)}")
    print(f"  Identification candidates: {len(identification_candidates)}")


if __name__ == "__main__":
    main()
