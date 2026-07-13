#!/usr/bin/env python3
"""Normalize ghost ephemeral URLs in artifact JSON files to relative paths.

PROBLEM: face_analysis.json and video_frame_analysis.json contain `url` fields
like `http://172.17.0.1:18200/photos/photo_100.jpg` — a transient HTTP server
that died when the script exited. The URL is permanently dead, but the actual
file exists on disk (just at a relative path, not an HTTP URL).

This script rewrites every `url` field in those JSONs to a relative path that
points to the file on disk. It also verifies each file exists.

Idempotent: re-running it is a no-op once urls are already relative paths.
Generic: works on any dataset; no hardcoded filenames.

Files normalized:
    face_analysis.json          url -> photos/<photo>
    video_frame_analysis.json   url -> video_frames/<frame>

Usage:
    python3 normalize_artifact_urls.py [RUN_DIR]
"""
import json
import os
import sys
from pathlib import Path

DEFAULT_RUN = Path(__file__).parent.parent / "artifacts" / "run-2026-07-12"


def _verify(rel_path: str, run_dir: Path) -> bool:
    """Check that a relative path resolves to a real file. Searches run_dir,
    any dataset under data/, entities/.../photos, and face_clusters/*/photos
    (face-bearing photos get copied into cluster dirs during clustering).

    Generic: works with ANY dataset — scans data/*/ for the source dir rather
    than hardcoding a specific export name."""
    candidates = [
        run_dir / rel_path,                                    # direct
        run_dir / "entities" / "text_and_scenes" / rel_path,   # copied photos
        run_dir / "entities" / rel_path,
    ]
    if any(c.is_file() for c in candidates):
        return True
    # Search ANY dataset under data/ (generic — not hardcoded to one export).
    # run_dir is .../orgs/specialists/deep-research-engine/artifacts/run-XX;
    # the workspace root is 5 levels up.
    workspace = run_dir
    for _ in range(6):
        workspace = workspace.parent
        if workspace.name == "workspace" or (workspace / "data").is_dir():
            break
    data_root = workspace / "data"
    if data_root.is_dir():
        for ds in data_root.iterdir():
            if ds.is_dir():
                for export in ds.iterdir():
                    if (export / rel_path).is_file():
                        return True
    # Photos with detected faces get organized into cluster folders. If
    # rel_path is photos/<name>, also scan entities/face_clusters/*/photos/.
    parts = rel_path.split("/", 1)
    if len(parts) == 2 and parts[0] == "photos":
        cluster_root = run_dir / "entities" / "face_clusters"
        if cluster_root.is_dir():
            return any((cd / "photos" / parts[1]).is_file()
                       for cd in cluster_root.iterdir())
    return False


def normalize_face_analysis(run_dir: Path) -> dict:
    """Rewrite url in face_analysis.json entries."""
    path = run_dir / "face_analysis.json"
    if not path.exists():
        return {"file": "face_analysis.json", "status": "missing"}
    data = json.loads(path.read_text())
    fixed, verified, missing = 0, 0, 0
    for entry in data:
        photo = entry.get("photo")
        if not photo:
            continue
        rel = f"photos/{photo}"
        was_ghost = isinstance(entry.get("url"), str) and entry["url"].startswith("http")
        entry["url"] = rel  # normalize (idempotent)
        if _verify(rel, run_dir):
            verified += 1
        else:
            missing += 1
        if was_ghost:
            fixed += 1
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False))
    return {"file": "face_analysis.json", "entries": len(data),
            "ghost_urls_replaced": fixed, "files_verified": verified,
            "files_missing": missing}


def normalize_video_frame_analysis(run_dir: Path) -> dict:
    """Rewrite url in video_frame_analysis.json entries."""
    path = run_dir / "video_frame_analysis.json"
    if not path.exists():
        return {"file": "video_frame_analysis.json", "status": "missing"}
    data = json.loads(path.read_text())
    fixed, verified, missing = 0, 0, 0
    for entry in data:
        frame = entry.get("frame")
        if not frame:
            continue
        rel = f"video_frames/{frame}"
        was_ghost = isinstance(entry.get("url"), str) and entry["url"].startswith("http")
        entry["url"] = rel
        if _verify(rel, run_dir):
            verified += 1
        else:
            missing += 1
        if was_ghost:
            fixed += 1
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False))
    return {"file": "video_frame_analysis.json", "entries": len(data),
            "ghost_urls_replaced": fixed, "files_verified": verified,
            "files_missing": missing}


def main():
    run_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_RUN
    print(f"=== normalize_artifact_urls: {run_dir} ===")
    print()
    for fn in (normalize_face_analysis, normalize_video_frame_analysis):
        r = fn(run_dir)
        if r.get("status") == "missing":
            print(f"  {r['file']}: not present (skip)")
        else:
            print(f"  {r['file']}: {r['entries']} entries, "
                  f"{r['ghost_urls_replaced']} ghost URLs replaced, "
                  f"{r['files_verified']} verified on disk, "
                  f"{r['files_missing']} missing")
        print()


if __name__ == "__main__":
    main()
