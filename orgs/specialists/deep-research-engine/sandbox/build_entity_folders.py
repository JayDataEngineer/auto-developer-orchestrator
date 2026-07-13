#!/usr/bin/env python3
"""Build per-entity folders for browsing.

For each face cluster: photos + faces.json + info.md (text + image combined)
For each voice cluster: audio segments + info.md

NO fancy naming. Plain folders. Sorted so a human can browse quickly.

Output: <run_dir>/entities/face_cluster_N/ and voice_cluster_N/
"""
import json
import os
import re
import shutil
import urllib.request
from pathlib import Path

PHOTOS_DIR = Path(os.environ.get("PHOTOS_DIR",
    "/sandbox/workspace/data/telegram-dump/Raw_ChatExport_2026-03-13/photos"))
AUDIO_DIR = Path(os.environ.get("AUDIO_DIR",
    "/sandbox/workspace/data/telegram-dump/ChatExport_2026-03-13/extracted_audio"))
RUN_DIR = Path(os.environ.get("RUN_DIR",
    "/sandbox/workspace/orgs/specialists/deep-research-engine/artifacts/run-2026-07-12"))
OUT_DIR = RUN_DIR / "entities"


def build_face_folders():
    """For each face cluster, create a folder with the source photos + text."""
    fa = json.loads((RUN_DIR / "face_analysis.json").read_text())
    fc = json.loads((RUN_DIR / "face_clusters.json").read_text())
    labels = fc["labels"]

    # Build ordered (photo, face_idx) list aligned with labels
    ordered = []
    for entry in fa:
        embs = entry.get("embeddings") or []
        for idx in range(len(embs)):
            ordered.append((entry["photo"], idx, entry.get("faces", [])))
    # Align
    n = min(len(ordered), len(labels))
    clusters = {}
    for i in range(n):
        cid = labels[i]
        if cid == -1:
            continue
        photo, face_idx, faces_list = ordered[i]
        # Face bbox if available
        bbox = faces_list[face_idx] if face_idx < len(faces_list) else None
        clusters.setdefault(cid, []).append({"photo": photo, "face_idx": face_idx, "bbox": bbox})

    face_out = OUT_DIR / "face_clusters"
    face_out.mkdir(parents=True, exist_ok=True)
    summary_lines = ["# Face clusters", ""]
    for cid in sorted(clusters):
        members = clusters[cid]
        cdir = face_out / f"cluster_{cid:02d}"
        (cdir / "photos").mkdir(parents=True, exist_ok=True)
        # Copy each photo
        copied = []
        for m in members:
            src = PHOTOS_DIR / m["photo"]
            if not src.exists():
                continue
            dst = cdir / "photos" / m["photo"]
            if not dst.exists():
                shutil.copy2(src, dst)
            copied.append(m["photo"])
        # info.md — the text the user can read
        info = [f"# Face cluster {cid}", "",
                f"**Photos:** {len(copied)}", f"**Faces in cluster:** {len(members)}", "",
                "## Photos in this cluster", ""]
        for p in copied:
            info.append(f"- `{p}`")
        info += ["", "## Face bounding boxes", "",
                 "```json", json.dumps(members, indent=2), "```"]
        (cdir / "info.md").write_text("\n".join(info))
        summary_lines.append(f"- [cluster_{cid:02d}](cluster_{cid:02d}/info.md) — {len(copied)} photos, {len(members)} faces")
    (face_out / "index.md").write_text("\n".join(summary_lines))
    print(f"face clusters: {len(clusters)} folders, {sum(len(v) for v in clusters.values())} face appearances")


def build_voice_folders():
    """If voice_clusters.json exists, build voice-cluster folders with audio."""
    vc_path = RUN_DIR / "voice_clusters.json"
    if not vc_path.exists():
        print("voice clusters: no voice_clusters.json — run voice_embed.py first")
        return
    vc = json.loads(vc_path.read_text())
    labels = vc["labels"]
    # vc must have an "sources" list naming the audio file per label
    sources = vc.get("sources", [])
    if not sources:
        print("voice clusters: no sources list — cannot map labels to files")
        return

    clusters = {}
    for i, (cid, src) in enumerate(zip(labels, sources)):
        if cid == -1:
            continue
        clusters.setdefault(cid, []).append(src)

    voice_out = OUT_DIR / "voice_clusters"
    voice_out.mkdir(parents=True, exist_ok=True)
    summary_lines = ["# Voice clusters", ""]
    for cid in sorted(clusters):
        members = clusters[cid]
        cdir = voice_out / f"cluster_{cid:02d}"
        (cdir / "audio").mkdir(parents=True, exist_ok=True)
        copied = []
        for src in members:
            # src is a basename or full path; resolve under AUDIO_DIR
            src_name = Path(src).name
            src_path = AUDIO_DIR / src_name
            if not src_path.exists():
                # try raw path
                src_path = Path(src)
            if not src_path.exists():
                continue
            dst = cdir / "audio" / src_name
            if not dst.exists():
                shutil.copy2(src_path, dst)
            copied.append(src_name)
        info = [f"# Voice cluster {cid}", "",
                f"**Audio files:** {len(copied)}", "",
                "## Files in this cluster", ""]
        for c in copied:
            info.append(f"- `{c}`")
        (cdir / "info.md").write_text("\n".join(info))
        summary_lines.append(f"- [cluster_{cid:02d}](cluster_{cid:02d}/info.md) — {len(copied)} audio files")
    (voice_out / "index.md").write_text("\n".join(summary_lines))
    print(f"voice clusters: {len(clusters)} folders, {sum(len(v) for v in clusters.values())} segments")


if __name__ == "__main__":
    print(f"=== build_entity_folders: {OUT_DIR} ===")
    build_face_folders()
    build_voice_folders()
    print("=== done ===")
