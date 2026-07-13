#!/usr/bin/env python3
"""Extract keyframes from EVERY source video via ffmpeg scene detection.

Wraps video_frames.py extract-scenes over all MP4 files. Skips videos that
already have frames. Writes keyframes + a per-video info card to
entities/video_frames/.

Resumable: if frames exist for a video stem, it's skipped.
"""
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

VIDEO_DIR = Path(os.environ.get("VIDEO_DIR",
    "/sandbox/workspace/data/telegram-dump/Raw_ChatExport_2026-03-13/video_files"))
RUN_DIR = Path(os.environ.get("RUN_DIR",
    "/sandbox/workspace/orgs/specialists/deep-research-engine/artifacts/run-2026-07-12"))
VFRAMES = RUN_DIR / "video_frames"
VFRAME_SCRIPT = Path(__file__).parent / "video_frames.py"
OUT_DIR = RUN_DIR / "entities" / "video_frames"


def main():
    videos = sorted(list(VIDEO_DIR.glob("*.MP4")) + list(VIDEO_DIR.glob("*.mp4"))
                    + list(VIDEO_DIR.glob("*.MOV")) + list(VIDEO_DIR.glob("*.mov")))
    print(f"found {len(videos)} source videos")
    VFRAMES.mkdir(parents=True, exist_ok=True)
    OUT_DIR.mkdir(parents=True, exist_ok=True)

    results = []
    for i, v in enumerate(videos):
        stem = v.stem
        # Skip if frames already exist for this video
        existing = list(VFRAMES.glob(f"{stem}__*.jpg"))
        if existing:
            print(f"  [{i+1}/{len(videos)}] {stem}: {len(existing)} frames already exist, skipping")
            results.append({"video": stem, "frames": len(existing), "stems": [f.name for f in existing]})
            continue
        # Extract to a temp work dir
        work = Path(f"/tmp/vf_{stem}")
        if work.exists():
            shutil.rmtree(work)
        try:
            proc = subprocess.run(
                ["python3", str(VFRAME_SCRIPT), "extract-scenes",
                 "--video", str(v), "--output", str(work)],
                capture_output=True, text=True, timeout=120)
            if proc.returncode != 0:
                print(f"  [{i+1}/{len(videos)}] {stem}: FAILED {proc.stderr[-200:]}", flush=True)
                results.append({"video": stem, "error": proc.stderr[-200:]})
                continue
            # Read frames.json for metadata
            meta = json.loads((work / "frames.json").read_text())
            # Rename frames to <stem>__<NN>.jpg and copy to VFRAMES
            moved = []
            for j, fm in enumerate(meta):
                src = Path(fm["path"])
                dst_name = f"{stem}__{j+1:03d}.jpg"
                dst = VFRAMES / dst_name
                # Convert png -> jpg to save space + match existing convention
                if src.suffix == ".png":
                    subprocess.run(["ffmpeg", "-y", "-i", str(src),
                                    "-q:v", "2", str(dst)], capture_output=True, timeout=30)
                else:
                    shutil.copy2(src, dst)
                moved.append({"frame": dst_name, "pts_time": fm.get("pts_time"),
                              "scene_score": fm.get("scene_score")})
            shutil.rmtree(work, ignore_errors=True)
            print(f"  [{i+1}/{len(videos)}] {stem}: {len(moved)} keyframes extracted", flush=True)
            results.append({"video": stem, "frames": len(moved), "stems": moved})
        except Exception as e:
            print(f"  [{i+1}/{len(videos)}] {stem}: EXCEPTION {e}", flush=True)
            results.append({"video": stem, "error": str(e)[:200]})

    # Write index
    lines = ["# Video keyframes (ffmpeg scene detection)", "",
             f"**Videos:** {len(videos)}", f"**Total keyframes:** {sum(len(r.get('stems',[])) for r in results)}", "",
             "Each video had keyframes extracted via ffmpeg `select='gt(scene,T)+not(mod(n,N))'` — scene-change detection plus a temporal fallback so single-take videos aren't missed.", ""]
    for r in results:
        if "error" in r:
            lines.append(f"- **{r['video']}** — ERROR: {r['error'][:80]}")
            continue
        n = r.get("frames", 0)
        lines.append(f"### {r['video']} ({n} keyframes)")
        for fm in r.get("stems", []):
            if isinstance(fm, str):
                lines.append(f"  - `{fm}`")
            else:
                t = fm.get("pts_time", "?")
                s = fm.get("scene_score", "?")
                lines.append(f"  - `{fm['frame']}` — t={t}s, scene_score={s}")
        lines.append("")
    (OUT_DIR / "info.md").write_text("\n".join(lines))
    total_frames = sum(len(r.get("stems", [])) for r in results)
    print(f"\nwrote {OUT_DIR}/info.md — {total_frames} total keyframes across {len(results)} videos")


if __name__ == "__main__":
    main()
