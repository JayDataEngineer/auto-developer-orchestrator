#!/usr/bin/env python3
"""Video keyframe extraction + face-embedding + VLM narrative client.

Ported from the legacy deep-research-engine `extract_scene_frames` +
`batch_vlm_video_analysis` pipeline (multimodal.py:866 / ingestion.py:488).
Replaces the naive `ffmpeg -vf fps=1` placeholder in INGEST_FACE_CLUSTERING_V2.

Three stages, each independently resumable:

  1. extract-scenes  — ffmpeg scene-detection + temporal fallback, writes
                       keyframe PNGs + parsed metadata (pts_time, scene_score)
  2. analyze-frames  — for each keyframe, embed_faces via media-mcp, write
                       face_appearance records to SurrealDB
  3. narrate         — batched VLM call on the sorted keyframes for a single
                       cohesive narrative description of the video content

Usage:
    # Full pipeline — extract + embed + narrate
    python3 video_frames.py process --video video.mp4 --source-id source:abc \\
        --item-id item:xyz --output /sandbox/workspace/video_work/video_stem

    # Stage-by-stage (resumable)
    python3 video_frames.py extract-scenes --video video.mp4 --output work/
    python3 video_frames.py analyze-frames --work work/ --source-id source:abc \\
        --item-id item:xyz
    python3 video_frames.py narrate --work work/

Environment:
    MEDIA_MCP_URL     Base URL of the media-analysis MCP.
                      Default: http://localhost:8101 (set via sandbox env
                      when MCP is hosted elsewhere)
    SURREALDB_URL     SurrealDB HTTP endpoint (surreal_client.py reads this)

Why scene-detection + temporal fallback, not fps=1:
    fps=1 blindly samples one frame per second. Long static scenes waste
    compute; fast cuts miss content between samples. The dual filter
    `select='gt(scene,T)+not(mod(n,N))'` captures every scene change AND
    forces a frame every N frames even without changes. Critical for
    single-take videos (vlogs, screen recordings, security feeds).
"""

import argparse
import base64
import contextlib
import http.server
import json
import os
import re
import socket
import socketserver
import subprocess
import sys
import threading
import urllib.parse
import urllib.request
from pathlib import Path


# ---------------------------------------------------------------------------
# Config

def get_mcp_url():
    return os.environ.get(
        "MEDIA_MCP_URL",
        "http://localhost:8101",
    ).rstrip("/")


# ---------------------------------------------------------------------------
# HTTP server (so a remote media-mcp can fetch keyframe images)

class _Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        root = self.server.root_dir  # type: ignore[attr-defined]
        path = (root / self.path.lstrip("/")).resolve()
        try:
            path.relative_to(root.resolve())
        except ValueError:
            self.send_response(403); self.end_headers(); return
        if not path.is_file():
            self.send_response(404); self.end_headers(); return
        self.send_response(200)
        self.send_header("Content-Type", "image/png")
        self.send_header("Content-Length", str(path.stat().st_size))
        self.end_headers()
        with path.open("rb") as f:
            self.wfile.write(f.read())

    def log_message(self, *a, **k):
        pass


class _Server(socketserver.TCPServer):
    allow_reuse_address = True


def _detect_public_ip():
    """Resolve the address a remote MCP can reach us at.

    Priority: AUDIO_HTTP_PUBLIC env (shared with audio_client.py) →
    tailscale ip -4 → hostname -I scan for 100.x.x.x → docker bridge.
    """
    explicit = os.environ.get("AUDIO_HTTP_PUBLIC", "").strip()
    if explicit:
        return explicit
    try:
        r = subprocess.run(["tailscale", "ip", "-4"],
                           capture_output=True, text=True, timeout=2)
        if r.returncode == 0:
            for line in r.stdout.strip().splitlines():
                if line.strip().startswith("100."):
                    return line.strip()
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass
    try:
        r = subprocess.run(["hostname", "-I"],
                           capture_output=True, text=True, timeout=2)
        if r.returncode == 0:
            for ip in r.stdout.strip().split():
                if ip.startswith("100."):
                    return ip
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass
    return "172.17.0.1"


@contextlib.contextmanager
def _serve_dir(directory: Path):
    """Expose `directory` over HTTP on a free port. Yields the base URL."""
    server = _Server(("0.0.0.0", 0), _Handler)
    server.root_dir = directory  # type: ignore[attr-defined]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    ip = _detect_public_ip()
    port = server.server_address[1]
    try:
        yield f"http://{ip}:{port}"
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


# ---------------------------------------------------------------------------
# Stage 1: extract-scene-frames (pure ffmpeg, no MCP)

def _parse_metadata(time_txt: Path) -> dict[int, dict]:
    """Parse ffmpeg metadata=print output. Returns {frame_idx: {pts_time, scene_score}}."""
    if not time_txt.exists():
        return {}
    content = time_txt.read_text(errors="replace")
    out: dict[int, dict] = {}
    blocks = re.split(r"frame:\d+", content)
    idx = 1
    for block in blocks:
        if not block.strip():
            continue
        pts = re.search(r"pts_time:([\d.]+)", block)
        score = re.search(r"lavfi\.scene_score=([\d.]+)", block)
        if pts:
            out[idx] = {
                "pts_time": float(pts.group(1)),
                "scene_score": float(score.group(1)) if score else 0.0,
            }
            idx += 1
    return out


def extract_scenes(video: Path, out_dir: Path,
                   threshold: float = 0.3, interval: int = 150) -> list[dict]:
    """Extract scene-change keyframes + temporal fallback frames.

    Filter: select='gt(scene,T)+not(mod(n,N))'
      - scene change captures every cut
      - not(mod(n,N)) forces a frame every N frames (~5s at 30fps)
        so single-take videos still get periodic samples
    """
    out_dir.mkdir(parents=True, exist_ok=True)
    metadata_file = out_dir / "time.txt"
    select_filter = (
        f"select='gt(scene,{threshold})+not(mod(n,{interval}))'"
    )
    cmd = [
        "ffmpeg", "-y", "-i", str(video),
        "-filter_complex",
        f"{select_filter},metadata=print:file={metadata_file}",
        "-vsync", "vfr",
        str(out_dir / "frame_%04d.png"),
    ]
    proc = subprocess.run(cmd, capture_output=True)
    if proc.returncode != 0:
        print(f"  ffmpeg FAIL: {proc.stderr.decode()[-300:]}", file=sys.stderr)
        return []

    pts_times = _parse_metadata(metadata_file)
    frames = []
    for i, f in enumerate(sorted(out_dir.glob("frame_*.png")), 1):
        meta = pts_times.get(i, {"pts_time": 0.0, "scene_score": 0.0})
        frames.append({
            "path": str(f),
            "filename": f.name,
            "pts_time": meta["pts_time"],
            "scene_score": meta["scene_score"],
        })

    (out_dir / "frames.json").write_text(json.dumps(frames, indent=2))
    print(f"  extracted {len(frames)} keyframes → {out_dir}")
    return frames


# ---------------------------------------------------------------------------
# Stage 2: analyze-frames (embed_faces per frame → SurrealDB)

def _mcp_post(endpoint: str, payload: dict, timeout: int = 120) -> dict:
    url = f"{get_mcp_url()}{endpoint}"
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url, data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode("utf-8"))


def _insert_face_appearance(item_id: str, source_id: str,
                             frame_path: str, pts_time: float,
                             bbox: list, embedding: list) -> None:
    """Insert a face_appearance record via surreal_client.py."""
    rec = {
        "item_id": item_id,
        "source_id": source_id,
        "frame_path": frame_path,
        "frame_sec": pts_time,
        "bbox": bbox,
        "embedding": embedding,
    }
    payload = json.dumps(rec)
    subprocess.run(
        ["python3", _surreal_client_path(), "insert",
         "--table", "face_appearance", "--record", payload],
        capture_output=True, check=False,
    )


def _surreal_client_path() -> str:
    """Resolve surreal_client.py via the canonical paths.sandbox_module() helper.

    Falls back to the legacy candidate chain if paths.py isn't available
    (e.g. test environments without init_files copied in).
    """
    try:
        from paths import sandbox_module
        return str(sandbox_module("surreal_client.py"))
    except ImportError:
        for candidate in ("surreal_client.py",):
            if os.path.exists(candidate):
                return candidate
        return "surreal_client.py"


def analyze_frames(work_dir: Path, source_id: str, item_id: str) -> dict:
    """Embed faces in each keyframe, write face_appearance records."""
    frames_json = work_dir / "frames.json"
    if not frames_json.exists():
        print(f"  no frames.json in {work_dir} — run extract-scenes first",
              file=sys.stderr)
        return {"status": "no_frames"}

    frames = json.loads(frames_json.read_text())
    cache_path = work_dir / "faces.json"
    cache = json.loads(cache_path.read_text()) if cache_path.exists() else []

    # Resume: skip frames we've already processed (matched by filename)
    done = {entry["filename"] for entry in cache}
    pending = [f for f in frames if f["filename"] not in done]
    if not pending:
        print(f"  all {len(frames)} frames already analyzed — cache hit")
    else:
        print(f"  analyzing {len(pending)}/{len(frames)} frames "
              f"({len(done)} cached)")

    with _serve_dir(work_dir) as base_url:
        for fi, frame in enumerate(pending, 1):
            filename = frame["filename"]
            url = f"{base_url}/{filename}"
            try:
                resp = _mcp_post("/embed_faces", {
                    "imageSource": url,
                    "max_faces": 10,
                }, timeout=60)
            except Exception as e:
                print(f"  [{fi}/{len(pending)}] {filename}: MCP FAIL {e}",
                      file=sys.stderr)
                continue

            faces = resp.get("faces") or []
            for face in faces:
                emb = face.get("embedding")
                bbox = face.get("bbox") or []
                if not emb:
                    continue
                _insert_face_appearance(
                    item_id=item_id,
                    source_id=source_id,
                    frame_path=frame["path"],
                    pts_time=frame["pts_time"],
                    bbox=bbox,
                    embedding=emb,
                )
                cache.append({
                    "filename": filename,
                    "pts_time": frame["pts_time"],
                    "bbox": bbox,
                    "embedding_dim": len(emb),
                })

            if fi % 10 == 0 or fi == len(pending):
                cache_path.write_text(json.dumps(cache, indent=2))
                print(f"  [{fi}/{len(pending)}] {filename}: "
                      f"{len(faces)} face(s) embedded")

    cache_path.write_text(json.dumps(cache, indent=2))
    return {
        "status": "ok",
        "frames_total": len(frames),
        "frames_analyzed": len(frames) - len(done) + len(
            [f for f in pending if any(c["filename"] == f["filename"]
                                       for c in cache)]),
        "faces_embedded": len(cache),
    }


# ---------------------------------------------------------------------------
# Stage 3: narrate (batched VLM call on sorted keyframes)

def _mcp_vlm(prompt: str, image_urls: list[str], timeout: int = 300) -> str:
    """Call vlm_vision on the media MCP with N images in a single batch."""
    if not image_urls:
        return ""
    # The MCP exposes a single-image vlm_vision; for batches, send one call
    # per image and let the caller synthesize. This matches the legacy
    # batch_vlm_video_analysis pattern's behavior when batches exceed the
    # VLM context window.
    parts = []
    for url in image_urls:
        try:
            resp = _mcp_post("/vlm_vision", {
                "imageSource": url,
                "prompt": prompt,
                "max_tokens": 800,
            }, timeout=timeout)
            text = resp.get("text") or resp.get("content") or ""
            parts.append(text.strip())
        except Exception as e:
            parts.append(f"[VLM failed for one frame: {e}]")
    return "\n\n".join(p for p in parts if p)


def narrate(work_dir: Path, prompt: str | None = None) -> str:
    """Generate a narrative description of the video from its keyframes."""
    frames_json = work_dir / "frames.json"
    if not frames_json.exists():
        return "[No frames — run extract-scenes first]"

    frames = json.loads(frames_json.read_text())
    if not frames:
        return "[No keyframes extracted]"

    frames_sorted = sorted(frames, key=lambda x: x.get("pts_time", 0))
    default_prompt = (
        "These are key frames from a video. Focus on the CONTENT: extract "
        "readable text, chat messages, profile names, URLs, and substantive "
        "information shown. Ignore phone UI elements. Summarize what the "
        "video is actually showing — who is in it, what is being discussed, "
        "what information is being shared."
    )
    the_prompt = prompt or default_prompt

    cache_path = work_dir / "narrative.txt"
    if cache_path.exists():
        print("  narrative cache hit")
        return cache_path.read_text()

    with _serve_dir(work_dir) as base_url:
        # Cap batch at 30 frames to bound runtime. Scene detection usually
        # produces <30 keyframes for typical chat videos; longer videos get
        # the first 30 chronologically.
        urls = [f"{base_url}/{f['filename']}" for f in frames_sorted[:30]]
        narrative = _mcp_vlm(the_prompt, urls)

    cache_path.write_text(narrative)
    print(f"  narrative written ({len(narrative)} chars) → {cache_path}")
    return narrative


# ---------------------------------------------------------------------------
# Full pipeline

def process_video(video: Path, out_dir: Path, source_id: str,
                  item_id: str, threshold: float, interval: int,
                  skip_narrate: bool) -> dict:
    """Run all three stages. Returns a summary dict."""
    print(f"\n=== video_frames: {video.name} ===")
    print(f"  output: {out_dir}")
    frames = extract_scenes(video, out_dir, threshold, interval)
    if not frames:
        return {"status": "extract_failed", "video": str(video)}
    analyze = analyze_frames(out_dir, source_id=source_id, item_id=item_id)
    summary = {
        "video": str(video),
        "frames": len(frames),
        "analyze": analyze,
    }
    if not skip_narrate:
        summary["narrative"] = narrate(out_dir)[:300] + "..."
    return summary


# ---------------------------------------------------------------------------
# CLI

def main():
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = p.add_subparsers(dest="cmd", required=True)

    pe = sub.add_parser("extract-scenes",
                        help="ffmpeg scene-detection + temporal keyframes")
    pe.add_argument("--video", required=True, type=Path)
    pe.add_argument("--output", required=True, type=Path,
                    help="Working dir for keyframes + metadata")
    pe.add_argument("--threshold", type=float, default=0.3,
                    help="Scene-change score threshold (default 0.3)")
    pe.add_argument("--interval", type=int, default=150,
                    help="Force a frame every N frames (default 150 ≈ 5s@30fps)")

    pa = sub.add_parser("analyze-frames",
                        help="embed_faces per keyframe → SurrealDB")
    pa.add_argument("--work", required=True, type=Path,
                    help="Working dir from extract-scenes")
    pa.add_argument("--source-id", required=True)
    pa.add_argument("--item-id", required=True)

    pn = sub.add_parser("narrate",
                        help="Batched VLM call on sorted keyframes")
    pn.add_argument("--work", required=True, type=Path)
    pn.add_argument("--prompt", default=None,
                    help="Override the default VLM prompt")

    pp = sub.add_parser("process", help="Full pipeline (all three stages)")
    pp.add_argument("--video", required=True, type=Path)
    pp.add_argument("--output", required=True, type=Path)
    pp.add_argument("--source-id", required=True)
    pp.add_argument("--item-id", required=True)
    pp.add_argument("--threshold", type=float, default=0.3)
    pp.add_argument("--interval", type=int, default=150)
    pp.add_argument("--skip-narrate", action="store_true",
                    help="Skip VLM narrative (saves MCP calls)")

    args = p.parse_args()

    if args.cmd == "extract-scenes":
        frames = extract_scenes(args.video, args.output,
                                args.threshold, args.interval)
        print(json.dumps({"frames": len(frames),
                          "output": str(args.output)}, indent=2))
        return 0 if frames else 1

    if args.cmd == "analyze-frames":
        out = analyze_frames(args.work,
                             source_id=args.source_id, item_id=args.item_id)
        print(json.dumps(out, indent=2))
        return 0 if out.get("status") == "ok" else 1

    if args.cmd == "narrate":
        text = narrate(args.work, args.prompt)
        print(text)
        return 0

    if args.cmd == "process":
        out = process_video(
            args.video, args.output,
            source_id=args.source_id, item_id=args.item_id,
            threshold=args.threshold, interval=args.interval,
            skip_narrate=args.skip_narrate,
        )
        print(json.dumps(out, indent=2, default=str))
        return 0

    return 2


if __name__ == "__main__":
    sys.exit(main())
