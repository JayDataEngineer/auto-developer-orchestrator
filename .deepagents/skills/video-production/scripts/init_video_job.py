#!/usr/bin/env python3
"""Create a clean, reusable workspace for a video-production job."""
import argparse
import json
import os
import re
from datetime import datetime
from pathlib import Path

# Default ROOT resolution priority:
#   1. $VIDEO_PRODUCTION_ROOT env var (explicit override)
#   2. /sandbox/workspace/video-productions (Pux sandbox — bind-mounted to host,
#      survives container deletion). Preferred when /sandbox/workspace is a real
#      directory (not a symlink to /app/workspace as in the dedicated container).
#   3. /workspace/video-productions (dedicated video-production container path;
#      bind-mounted via the named volume in research-video-producer)
#   4. ~/video-productions (fallback for host-side dev)
def _detect_default_root() -> str:
    pux_bind = Path("/sandbox/workspace/video-productions")
    dedicated = Path("/workspace/video-productions")
    # In pux-sandbox, /sandbox/workspace is a real directory bind-mounted to host.
    # In research-video-producer, it's a symlink to /app/workspace (NOT bind-mounted).
    pux_bind_active = (
        Path("/sandbox/workspace").is_dir() and not Path("/sandbox/workspace").is_symlink()
    )
    if pux_bind_active:
        candidates = [pux_bind, dedicated, Path.home() / "video-productions"]
    else:
        candidates = [dedicated, pux_bind, Path.home() / "video-productions"]
    for c in candidates:
        try:
            c.mkdir(parents=True, exist_ok=True)
            return str(c)
        except OSError:
            continue
    return str(Path.home() / "video-productions")

ROOT = Path(os.environ.get("VIDEO_PRODUCTION_ROOT", _detect_default_root())).expanduser()


def slugify(text: str) -> str:
    slug = re.sub(r"[^a-zA-Z0-9]+", "-", text.lower()).strip("-")
    slug = re.sub(r"-+", "-", slug)
    return (slug or "video")[:64].strip("-") or "video"


def main():
    ap = argparse.ArgumentParser(description="Initialize a video production job workspace")
    ap.add_argument("title", help="Human-readable video topic/title")
    ap.add_argument("--slug", help="Optional stable slug")
    ap.add_argument("--prompt", help="Original user prompt/request")
    ap.add_argument("--source", action="append", default=[], help="Source URL/path/ID; can repeat")
    args = ap.parse_args()

    now = datetime.now().astimezone()
    slug = slugify(args.slug or args.title)
    job_id = f"{now.strftime('%Y-%m-%d-%H%M')}-{slug}"
    job = ROOT / "jobs" / job_id
    for sub in ["assets", "audio", "frames", "src", "renders", "logs", "exports"]:
        (job / sub).mkdir(parents=True, exist_ok=False)
    (ROOT / "backups").mkdir(parents=True, exist_ok=True)
    (ROOT / "packages").mkdir(parents=True, exist_ok=True)

    manifest = {
        "job_id": job_id,
        "title": args.title,
        "slug": slug,
        "created_at": now.isoformat(),
        "prompt": args.prompt or args.title,
        "sources": args.source,
        "status": "initialized",
        "final_video": None,
        "backup_video": None,
        "host_url": None,
        "notes": [],
    }
    (job / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    (job / "README.md").write_text(
        f"# {args.title}\n\nJob: `{job_id}`\n\nUse this folder for durable artifacts. Use `/tmp/video-production-{slug}/` for disposable intermediates when appropriate.\n",
        encoding="utf-8",
    )

    latest = ROOT / "current"
    if latest.exists() or latest.is_symlink():
        latest.unlink()
    latest.symlink_to(job, target_is_directory=True)

    print(job)


if __name__ == "__main__":
    main()
