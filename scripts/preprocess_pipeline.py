#!/usr/bin/env python3
"""Deterministic multimodal pre-processing pipeline.

Runs on the HOST with direct media-mcp access (localhost:8101). Processes ALL
media BEFORE the LLM agent starts — the agent receives structured artifacts
and does only reasoning (entity resolution, dossier building, synthesis).

Generic: works on ANY dataset structure. Auto-detects photos, audio, video.
Idempotent: checkpoints per step — re-running skips completed items.
Resumable: can Ctrl-C and resume — reads checkpoints from run_dir.

Usage:
    python3 scripts/preprocess_pipeline.py --data data/<export> --run-dir <path>
    python3 scripts/preprocess_pipeline.py ... --step faces     # one step only
    python3 scripts/preprocess_pipeline.py ... --list           # list steps

Environment:
    MEDIA_MCP_URL   default http://localhost:8101
    SURREALDB_URL   default http://localhost:8000
    OPENROUTER_API_KEY  passed to media-mcp for cloud_vlm calls
"""

import argparse
import base64
import hashlib
import http.server
import json
import os
import socketserver
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

# ─── Config ──────────────────────────────────────────────────────────────────

MCP_URL = os.environ.get("MEDIA_MCP_URL", "http://localhost:8101").rstrip("/")
SURREAL_URL = os.environ.get("SURREALDB_URL", "http://localhost:8000")
SURREAL_NS = os.environ.get("SURREALDB_NS", "research")
SURREAL_DB = os.environ.get("SURREALDB_DB", "main")
SURREAL_AUTH = os.environ.get("SURREALDB_AUTH", "")
MAX_WORKERS = int(os.environ.get("PREPROCESS_WORKERS", "4"))

PHOTO_EXTS = {".jpg", ".jpeg", ".png", ".webp", ".bmp", ".heic"}
AUDIO_EXTS = {".ogg", ".mp3", ".wav", ".m4a", ".opus", ".flac", ".aac", ".webm"}
VIDEO_EXTS = {".mp4", ".mov", ".mkv", ".avi", ".m4v"}
THUMB_MARKERS = {"_thumb", "_thumb_", "thumb_"}

# ─── MCP Client ──────────────────────────────────────────────────────────────

class MCPClient:
    """Media-mcp JSON-RPC client with auto session refresh and retries."""

    def __init__(self, url: str):
        self.url = url + "/mcp"
        self._session = None
        self._lock = threading.Lock()

    def _init_session(self):
        payload = json.dumps({
            "jsonrpc": "2.0", "id": 0,
            "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "preprocess", "version": "1"},
            },
        }).encode()
        req = urllib.request.Request(self.url, data=payload, headers={
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        })
        with self._lock:
            resp = urllib.request.urlopen(req, timeout=15)
            self._session = resp.headers.get("Mcp-Session-Id", "")
            # Drain the body
            resp.read()

    def call(self, tool: str, args: dict, timeout: int = 120, retries: int = 3):
        """Call a media-mcp tool. Returns parsed JSON result dict."""
        for attempt in range(retries):
            try:
                if not self._session:
                    self._init_session()
                payload = json.dumps({
                    "jsonrpc": "2.0", "id": 1,
                    "method": "tools/call",
                    "params": {"name": tool, "arguments": args},
                }).encode()
                req = urllib.request.Request(self.url, data=payload, headers={
                    "Content-Type": "application/json",
                    "Accept": "application/json, text/event-stream",
                    "Mcp-Session-Id": self._session or "",
                })
                resp = urllib.request.urlopen(req, timeout=timeout)
                raw = resp.read().decode()
                return self._parse_sse(raw)
            except Exception as e:
                if attempt < retries - 1:
                    self._session = None  # Force re-init
                    time.sleep(2 * (attempt + 1))
                else:
                    return {"error": f"{type(e).__name__}: {e}"}
        return {"error": "max retries exceeded"}

    @staticmethod
    def _parse_sse(raw: str):
        """Parse SSE/text response, return the JSON content from result."""
        for line in raw.split("\n"):
            if line.startswith("data: "):
                line = line[6:]
            try:
                d = json.loads(line)
                # Skip notification messages (no "result" key)
                if "result" not in d:
                    continue
                if "error" in d and "message" in d.get("error", {}):
                    return {"error": d["error"]["message"]}
                content = d.get("result", {}).get("content", [])
                for c in content:
                    if c.get("type") == "text":
                        try:
                            return json.loads(c["text"])
                        except (json.JSONDecodeError, TypeError):
                            return {"text": c["text"]}
                # Result present but no text content — return structuredContent
                struct = d.get("result", {}).get("structuredContent")
                if struct:
                    return struct
                return d.get("result", d)
            except (json.JSONDecodeError, TypeError):
                continue
        return {"error": "empty response"}


# ─── File Server (for audio — media-mcp fetches via URL) ─────────────────────

class FileServer:
    """Serves files from a directory. media-mcp (in Docker) fetches via URL.

    Binds to 0.0.0.0 so media-mcp inside the container can reach it via
    host.docker.internal (configured via extra_hosts in docker-compose).
    """

    def __init__(self, root: str, host: str = "0.0.0.0"):
        self.root = os.path.abspath(root)
        handler = type("Handler", (http.server.SimpleHTTPRequestHandler,), {
            "__init__": lambda s, *a, **k: http.server.SimpleHTTPRequestHandler.__init__(
                s, *a, directory=self.root, **k),
            "log_message": lambda *a: None,  # silence
        })
        self.httpd = socketserver.TCPServer((host, 0), handler)
        self.port = self.httpd.server_address[1]
        self._thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self._thread.start()

    def url_for(self, abs_path: str) -> str:
        """URL for media-mcp to fetch. Uses host.docker.internal since
        media-mcp runs inside a container and needs to reach the host."""
        rel = os.path.relpath(abs_path, self.root)
        import urllib.parse
        return f"http://host.docker.internal:{self.port}/{urllib.parse.quote(rel)}"

    def shutdown(self):
        self.httpd.shutdown()


# ─── Whisper ASR (direct, bypasses broken media-mcp Parakeet) ────────────────

class WhisperASR:
    """Direct ASR using faster-whisper (CTranslate2 backend).
    Bypasses media-mcp's Parakeet ONNX path (broken Add_2 node on all
    onnxruntime versions tested). faster-whisper is faster, more accurate,
    and works reliably on CPU with int8 quantization."""

    _model = None  # lazy singleton

    def __init__(self, model_size: str = "large-v3", device: str = "auto"):
        if WhisperASR._model is None:
            from faster_whisper import WhisperModel
            dev = device if device != "auto" else ("cuda" if _has_cuda() else "cpu")
            compute = "float16" if dev == "cuda" else "int8"
            progress(f"  Loading Whisper model: {model_size} ({dev}/{compute})")
            WhisperASR._model = WhisperModel(model_size, device=dev, compute_type=compute)
            progress(f"  Whisper model loaded")

    def transcribe(self, audio_path: str) -> dict:
        segments, info = self._transcribe_with_retry(audio_path)
        seg_list = [
            {"start": round(s.start, 2), "end": round(s.end, 2), "text": s.text.strip()}
            for s in segments
        ]
        text = " ".join(s["text"] for s in seg_list)
        return {"text": text, "segments": seg_list, "language": info.language}

    def _transcribe_with_retry(self, audio_path: str, retries: int = 2):
        for attempt in range(retries + 1):
            try:
                return WhisperASR._model.transcribe(
                    audio_path, beam_size=5, vad_filter=True,
                    vad_parameters=dict(min_silence_duration_ms=500),
                )
            except Exception as e:
                if attempt < retries:
                    progress(f"    retry {attempt+1}/{retries}: {str(e)[:60]}")
                    time.sleep(2)
                else:
                    raise


def _has_cuda() -> bool:
    try:
        import torch
        return torch.cuda.is_available()
    except ImportError:
        return False


# ─── Pyannote Voice (direct host-side, bypasses media-mcp) ───────────────────

HF_TOKEN = os.environ.get("HF_TOKEN", os.environ.get("HUGGING_FACE_HUB_TOKEN", ""))


class PyannoteVoice:
    """Host-side voice embeddings via WeSpeaker + diarization via pyannote 3.1.
    Bypasses media-mcp (which lacks pyannote.audio in its Docker image).
    Requires: pip install pyannote.audio + HF_TOKEN with accepted licenses."""

    _embed_model = None
    _dia_pipeline = None

    @classmethod
    def _get_embed_model(cls):
        if cls._embed_model is None:
            from pyannote.audio import Model
            cls._embed_model = Model.from_pretrained(
                "pyannote/wespeaker-voxceleb-resnet34-LM",
                token=HF_TOKEN,
            )
            progress("  Voice embed model loaded (WeSpeaker)")
        return cls._embed_model

    @classmethod
    def _get_dia_pipeline(cls):
        if cls._dia_pipeline is None:
            import torch
            from pyannote.audio import Pipeline
            cls._dia_pipeline = Pipeline.from_pretrained(
                "pyannote/speaker-diarization-3.1",
                token=HF_TOKEN,
            )
            # Move to GPU if available (5-10x speedup over CPU)
            if torch.cuda.is_available():
                cls._dia_pipeline.to(torch.device("cuda"))
                progress("  Diarization pipeline loaded (pyannote 3.1, GPU)")
            else:
                progress("  Diarization pipeline loaded (pyannote 3.1, CPU)")
        return cls._dia_pipeline

    @classmethod
    def embed(cls, audio_path: str) -> dict:
        """Extract a 256-d voice embedding from an audio file."""
        import torch, torchaudio
        from pyannote.audio import Inference
        model = cls._get_embed_model()
        waveform, sr = torchaudio.load(audio_path)
        if sr != 16000:
            waveform = torchaudio.transforms.Resample(sr, 16000)(waveform)
            sr = 16000
        # Use first 60 seconds max for embedding (speed)
        if waveform.shape[1] > 60 * sr:
            waveform = waveform[:, :60 * sr]
        inference = Inference(model, window="whole")
        emb = inference({"waveform": waveform, "sample_rate": sr})
        return {"embedding": emb.tolist() if hasattr(emb, "tolist") else list(emb)}

    @classmethod
    def diarize(cls, audio_path: str, min_speakers: int = 1, max_speakers: int = 4) -> dict:
        """Run full speaker diarization. Returns segments + speaker embeddings.
        max_speakers=4 covers most conversations while keeping clustering fast."""
        pipeline = cls._get_dia_pipeline()
        result = pipeline(audio_path, min_speakers=min_speakers, max_speakers=max_speakers)
        dia = result.speaker_diarization
        segments = []
        for turn, _, speaker in dia.itertracks(yield_label=True):
            segments.append({
                "speaker": speaker,
                "start": round(turn.start, 2),
                "end": round(turn.end, 2),
            })
        return {
            "segments": segments,
            "n_speakers": len(set(s["speaker"] for s in segments)),
            "speaker_embeddings": result.speaker_embeddings.tolist()
                if hasattr(result.speaker_embeddings, "tolist") else [],
        }


# ─── Utilities ───────────────────────────────────────────────────────────────

def find_media(data_dir: Path, exts: set, skip_thumbs: bool = True) -> list[Path]:
    """Find all media files recursively, optionally skipping thumbnails."""
    results = []
    for p in sorted(data_dir.rglob("*")):
        if p.is_file() and p.suffix.lower() in exts:
            name = p.name.lower()
            if skip_thumbs and any(m in name for m in THUMB_MARKERS):
                continue
            results.append(p)
    return results


def rel_key(data_dir: Path, p: Path) -> str:
    """Stable dedup key for a media path: relative-to-data_dir when possible.

    Two files in different subdirectories that share a basename (e.g.
    ``photos/foo.jpg`` and ``NotebookLM_Source/foo.jpg``) are DIFFERENT files
    and must BOTH be processed. Using ``p.name`` (basename only) for idempotency
    caused the second copy to be silently skipped, leaving DB records with
    missing embeddings/classification/OCR — failing audit coverage checks.
    """
    try:
        return str(p.relative_to(data_dir))
    except ValueError:
        return str(p)


def load_json(path: Path):
    if path.exists():
        try:
            return json.loads(path.read_text())
        except (json.JSONDecodeError, OSError):
            pass
    return None


def save_json(path: Path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False))


def file_to_data_uri(path: Path) -> str:
    ext = path.suffix.lower().lstrip(".")
    mime = {
        "jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png",
        "webp": "image/webp", "bmp": "image/bmp",
    }.get(ext, "application/octet-stream")
    b64 = base64.b64encode(path.read_bytes()).decode()
    return f"data:{mime};base64,{b64}"


def progress(msg: str):
    ts = time.strftime("%H:%M:%S")
    print(f"  [{ts}] {msg}", flush=True)


def run_step(name: str, fn, *args, **kwargs):
    """Run a pipeline step with timing and error handling."""
    progress(f"── {name} ── start")
    t0 = time.time()
    try:
        fn(*args, **kwargs)
        elapsed = time.time() - t0
        progress(f"── {name} ── done ({elapsed:.0f}s)")
        return True
    except Exception as e:
        elapsed = time.time() - t0
        progress(f"── {name} ── FAILED ({elapsed:.0f}s): {e}")
        import traceback
        traceback.print_exc()
        return False


# ─── SurrealDB Writer ────────────────────────────────────────────────────────

def surreal_query(sql: str):
    """Execute a SurrealQL statement via HTTP."""
    headers = {
        "Content-Type": "text/plain",
        "Accept": "application/json",
        "surreal-ns": SURREAL_NS,
        "surreal-db": SURREAL_DB,
    }
    if SURREAL_AUTH:
        headers["Authorization"] = SURREAL_AUTH
    else:
        import base64 as b64
        headers["Authorization"] = "Basic " + b64.b64encode(b"root:root").decode()
    req = urllib.request.Request(
        f"{SURREAL_URL}/sql", data=sql.encode(), headers=headers
    )
    resp = urllib.request.urlopen(req, timeout=30)
    return json.loads(resp.read())


# ─── Pipeline Steps ──────────────────────────────────────────────────────────

def step_parse(data_dir: Path, run_dir: Path):
    """Step 1: Parse Telegram export → items.json using telegram_parser.py."""
    items_path = run_dir / "items.json"
    if items_path.exists():
        existing = load_json(items_path)
        if existing and existing.get("stats", {}).get("total", 0) > 0:
            progress(f"  parse: items.json exists ({existing['stats']['total']} items), skip")
            return

    parser = Path(__file__).parent.parent / ".deepagents/skills/deep-research/scripts/telegram_parser.py"
    if not parser.exists():
        progress(f"  parse: parser not found at {parser}, skipping")
        return
    out = run_dir / "items.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    cmd = ["python3", str(parser), "parse", "--input", str(data_dir), "--output", str(out)]
    result = subprocess.run(cmd, capture_output=True, text=True, timeout=300)
    if result.returncode != 0:
        progress(f"  parse: parser exited {result.returncode}: {result.stderr[:200]}")
    else:
        d = load_json(out)
        n = d.get("stats", {}).get("total", 0) if d else 0
        progress(f"  parse: {n} items written to items.json")


def step_faces(client: MCPClient, data_dir: Path, run_dir: Path):
    """Step 2: Face embeddings for ALL photos via InsightFace (media-mcp).

    Uses the path RELATIVE to data_dir (not the basename) for idempotency
    tracking. Two photos in different subdirectories (e.g. ``photos/foo.jpg``
    and ``NotebookLM_Source/foo.jpg``) are DIFFERENT files and must BOTH get
    embeddings. The old code used ``photo.name`` (basename only), which caused
    the second copy to be silently skipped — leaving DB face_appearance rows
    with ``embedding = NONE`` and failing audit check #7 (embedding coverage).
    """
    out = run_dir / "face_embeddings.json"
    existing = load_json(out) or {"embeddings": [], "metadata": []}
    done = {m.get("photo") for m in existing.get("metadata", [])}

    photos = find_media(data_dir, PHOTO_EXTS)

    pending = [p for p in photos if rel_key(data_dir, p) not in done]
    progress(f"  faces: {len(photos)} photos total, {len(done)} done, {len(pending)} pending")

    if not pending:
        return

    def embed_photo(photo: Path):
        uri = file_to_data_uri(photo)
        result = client.call("embed_faces", {"imageSource": uri}, timeout=30)
        return photo, result

    batch_save = 0
    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as pool:
        futures = {pool.submit(embed_photo, p): p for p in pending}
        for fut in as_completed(futures):
            photo, result = fut.result()
            if "error" in result:
                progress(f"    {rel_key(data_dir, photo)}: ERROR {result['error'][:80]}")
                continue
            faces = result.get("faces", result.get("embeddings", []))
            for idx, face in enumerate(faces):
                emb = face.get("embedding", face.get("embedding_512", []))
                box = face.get("box", face.get("bbox", {}))
                if emb:
                    existing["embeddings"].append(emb)
                    existing["metadata"].append({
                        "photo": rel_key(data_dir, photo), "face_idx": idx,
                        "box": box, "confidence": face.get("confidence", face.get("score", 0)),
                    })
            batch_save += 1
            if batch_save % 20 == 0:
                save_json(out, existing)
                progress(f"    {batch_save}/{len(pending)} processed, {len(existing['embeddings'])} embeddings")

    save_json(out, existing)
    progress(f"  faces: {len(existing['embeddings'])} total embeddings from {len(existing['metadata'])} faces")


def step_face_clusters(client: MCPClient, run_dir: Path):
    """Step 3: Cluster face embeddings via HDBSCAN (media-mcp)."""
    emb_path = run_dir / "face_embeddings.json"
    out = run_dir / "face_clusters.json"
    data = load_json(emb_path)
    if not data or not data.get("embeddings"):
        progress("  face_clusters: no embeddings to cluster, skip")
        return

    embeddings = data["embeddings"]
    result = client.call("cluster_embeddings", {
        "embeddings": embeddings,
        "min_cluster_size": 2,
        "metric": "cosine",
    }, timeout=60)
    if "error" not in result:
        save_json(out, result)
        n = result.get("n_clusters", 0)
        n_noise = result.get("n_noise", 0)
        progress(f"  face_clusters: {n} clusters, {n_noise} noise points")
    else:
        progress(f"  face_clusters: clustering failed: {result['error'][:100]}")


def step_voice(client: MCPClient, server: FileServer, data_dir: Path, run_dir: Path):
    """Step 4: Voice embeddings for ALL audio via WeSpeaker (host-side pyannote)."""
    out = run_dir / "voice_embeddings.json"
    existing = load_json(out) or {"embeddings": [], "metadata": []}
    done = {m.get("audio") for m in existing.get("metadata", [])}

    # Dedup by stem: wav + m4a of same recording = 1
    all_audio = find_media(data_dir, AUDIO_EXTS)
    seen_stems = {}
    for a in all_audio:
        stem = a.stem.split("@")[0].split("_extracted")[0].lower()
        if stem not in seen_stems:
            seen_stems[stem] = a
    audio_files = sorted(seen_stems.values())
    pending = [a for a in audio_files if rel_key(data_dir, a) not in done]
    progress(f"  voice: {len(audio_files)} audio (deduped), {len(done)} done, {len(pending)} pending")

    if not pending:
        return

    for i, audio in enumerate(pending):
        try:
            result = PyannoteVoice.embed(str(audio))
            emb = result.get("embedding", [])
            if emb:
                existing["embeddings"].append(emb)
                akey = rel_key(data_dir, audio)
                existing["metadata"].append({"audio": akey, "path": str(audio)})
                progress(f"    [{len(existing['embeddings'])}/{len(audio_files)}] {akey}: {len(emb)}-d")
            else:
                progress(f"    {audio.name}: no embedding")
        except Exception as e:
            progress(f"    {audio.name}: ERROR {str(e)[:80]}")

    save_json(out, existing)
    progress(f"  voice: {len(existing['embeddings'])} voice embeddings")


def step_voice_clusters(client: MCPClient, run_dir: Path):
    """Step 5: Cluster voice embeddings."""
    emb_path = run_dir / "voice_embeddings.json"
    out = run_dir / "voice_clusters.json"
    data = load_json(emb_path)
    if not data or not data.get("embeddings"):
        progress("  voice_clusters: no embeddings, skip")
        return
    result = client.call("cluster_embeddings", {
        "embeddings": data["embeddings"],
        "min_cluster_size": 2, "metric": "cosine",
    }, timeout=60)
    if "error" not in result:
        save_json(out, result)
        progress(f"  voice_clusters: {result.get('n_clusters',0)} clusters")
    else:
        progress(f"  voice_clusters: failed: {result['error'][:100]}")


def step_asr(client: MCPClient, server: FileServer, data_dir: Path, run_dir: Path):
    """Step 6: Transcribe ALL audio via faster-whisper (direct, reliable).
    Bypasses media-mcp's broken Parakeet ONNX model (Add_2 node fails on
    all onnxruntime versions). faster-whisper uses CTranslate2 instead."""
    trans_dir = run_dir / "audio_transcripts"
    trans_dir.mkdir(parents=True, exist_ok=True)

    all_audio = find_media(data_dir, AUDIO_EXTS)
    # Dedup by stem
    seen = {}
    for a in all_audio:
        stem = a.stem.split("@")[0].lower()
        if stem not in seen:
            seen[stem] = a
    audio_files = sorted(seen.values())

    pending = []
    for a in audio_files:
        safe = a.stem.replace("/", "_").replace(" ", "_")
        out = trans_dir / f"{safe}.json"
        existing = load_json(out)
        if existing and existing.get("transcript") and not existing.get("error"):
            continue  # Already has real transcript
        pending.append((a, out))
    progress(f"  asr: {len(audio_files)} audio, {len(pending)} need transcription")

    if not pending:
        return

    # Use faster-whisper directly (not media-mcp)
    asr = WhisperASR()
    done = 0
    for audio, out_path in pending:
        try:
            result = asr.transcribe(str(audio))
            save_json(out_path, {
                "file": audio.name, "path": str(audio),
                "transcript": result["text"],
                "segments": result["segments"],
                "language": result.get("language", ""),
            })
            done += 1
            sample = result["text"][:60]
            progress(f"    [{done}/{len(pending)}] {audio.name}: \"{sample}...\"")
        except Exception as e:
            save_json(out_path, {
                "file": audio.name, "path": str(audio),
                "transcript": "", "error": str(e)[:200],
            })
            progress(f"    [{done}/{len(pending)}] {audio.name}: ERROR {str(e)[:60]}")

    progress(f"  asr: {done}/{len(pending)} transcribed")


def step_diarize(client: MCPClient, server: FileServer, data_dir: Path, run_dir: Path):
    """Step 7: Speaker diarization via pyannote 3.1 (host-side, direct).
    Adds speaker segments to each transcript file."""
    trans_dir = run_dir / "audio_transcripts"
    all_audio = find_media(data_dir, AUDIO_EXTS)
    seen = {}
    for a in all_audio:
        stem = a.stem.split("@")[0].lower()
        if stem not in seen:
            seen[stem] = a
    audio_files = sorted(seen.values())

    count = 0
    for a in audio_files:
        safe = a.stem.replace("/", "_").replace(" ", "_")
        trans_path = trans_dir / f"{safe}.json"
        trans = load_json(trans_path)
        if not trans or trans.get("diarization"):
            continue  # No transcript or already diarized
        try:
            result = PyannoteVoice.diarize(str(a))
            trans["diarization"] = result.get("segments", [])
            trans["n_speakers"] = result.get("n_speakers", 0)
            save_json(trans_path, trans)
            count += 1
            progress(f"    [{count}] {a.name}: {result.get('n_speakers',0)} speakers")
        except Exception as e:
            progress(f"    {a.name}: ERROR {str(e)[:80]}")

    progress(f"  diarize: {count} files diarized")


def step_keyframes(data_dir: Path, run_dir: Path):
    """Step 8: Extract keyframes from videos via ffmpeg (scene changes + interval)."""
    frames_dir = run_dir / "video_frames"
    frames_dir.mkdir(parents=True, exist_ok=True)

    videos = find_media(data_dir, VIDEO_EXTS)
    total = 0
    for v in videos:
        vname = v.stem
        vdir = frames_dir / vname
        if vdir.exists() and any(vdir.iterdir()):
            total += len(list(vdir.glob("*.jpg")))
            continue  # Already extracted
        vdir.mkdir(parents=True, exist_ok=True)
        # Extract ~1 frame per 5 seconds + at scene changes
        cmd = [
            "ffmpeg", "-i", str(v), "-vf",
            "fps=1/5,scale=640:-1", "-frames:v", "20",
            "-q:v", "2", str(vdir / "frame_%03d.jpg"),
            "-y", "-loglevel", "error",
        ]
        subprocess.run(cmd, capture_output=True, timeout=60)
        n = len(list(vdir.glob("*.jpg")))
        total += n
        progress(f"    {vname}: {n} keyframes")
    progress(f"  keyframes: {total} total keyframes from {len(videos)} videos")


def step_vlm_frames(client: MCPClient, run_dir: Path):
    """Step 9: VLM description of each video keyframe (MiMo multimodal)."""
    frames_dir = run_dir / "video_frames"
    out = run_dir / "video_frame_analysis.json"
    existing = load_json(out) or []
    done = {e.get("frame") for e in existing if "error" not in e}

    frames = sorted(frames_dir.rglob("*.jpg"))
    pending = [f for f in frames if rel_key(run_dir, f) not in done]
    progress(f"  vlm_frames: {len(frames)} keyframes, {len(pending)} pending")

    if not pending:
        return

    def describe(frame: Path):
        uri = file_to_data_uri(frame)
        video = frame.parent.name
        result = client.call("cloud_vlm", {
            "image": uri,
            "prompt": "Describe what you see in this video frame. Note: people (count, appearance, actions), objects, weapons, text visible on screen, setting/location, and any notable details.",
        }, timeout=60)
        return {"frame": rel_key(run_dir, frame), "video": video, "path": str(frame.relative_to(run_dir)),
                "description": result.get("response") or result.get("text") or result.get("description") or "",
                "error": result.get("error", "")}

    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as pool:
        futures = {pool.submit(describe, f): f for f in pending}
        for fut in as_completed(futures):
            entry = fut.result()
            existing.append(entry)
            if entry.get("error"):
                progress(f"    {entry['frame']}: ERROR")
            else:
                progress(f"    {entry['frame']}: OK")
    save_json(out, existing)
    ok = sum(1 for e in existing if not e.get("error"))
    progress(f"  vlm_frames: {ok}/{len(existing)} described")


def step_classify(client: MCPClient, data_dir: Path, run_dir: Path):
    """Step 10: Classify each photo (text_screenshot/photo_people/document/meme)."""
    out = run_dir / "image_classification.json"
    existing = load_json(out) or {}
    done = {k for k, v in existing.items() if v.get("category") and "error" not in v}

    photos = find_media(data_dir, PHOTO_EXTS)
    pending = [p for p in photos if rel_key(data_dir, p) not in done]
    progress(f"  classify: {len(photos)} photos, {len(pending)} pending")

    if not pending:
        return

    def classify_photo(photo: Path):
        uri = file_to_data_uri(photo)
        result = client.call("cloud_vlm", {
            "image": uri,
            "prompt": "Classify this image into exactly one category: 'text_screenshot' (a screenshot of a text conversation, social media post, or document), 'photo_people' (a photo showing people/faces), 'document' (a scanned document or form), 'meme' (a meme or edited image with text overlay), 'object' (a photo of an object, weapon, vehicle, or scene without people). Reply with just the category name.",
        }, timeout=30)
        cat = (result.get("response") or result.get("text") or "").strip().lower()
        for valid in ["text_screenshot", "photo_people", "document", "meme", "object"]:
            if valid in cat:
                return rel_key(data_dir, photo), {"category": valid}
        return rel_key(data_dir, photo), {"category": "other", "raw": cat[:50], "error": result.get("error", "")}

    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as pool:
        futures = {pool.submit(classify_photo, p): p for p in pending}
        for fut in as_completed(futures):
            name, info = fut.result()
            existing[name] = info
            if len(existing) % 50 == 0:
                save_json(out, existing)
                progress(f"    {len(existing)}/{len(photos)} classified")
    save_json(out, existing)
    from collections import Counter
    cats = Counter(v.get("category", "?") for v in existing.values())
    progress(f"  classify: {dict(cats)}")


def step_ocr(client: MCPClient, data_dir: Path, run_dir: Path):
    """Step 11: OCR on photos classified as text_screenshot."""
    cls_path = run_dir / "image_classification.json"
    out = run_dir / "ocr_results.json"
    classification = load_json(cls_path) or {}
    existing = load_json(out) or {}

    text_photos = [name for name, info in classification.items()
                   if info.get("category") == "text_screenshot"]
    # Also OCR photos that might contain text (documents, memes)
    for cat in ["document", "meme"]:
        text_photos += [name for name, info in classification.items()
                        if info.get("category") == cat]

    pending = [n for n in text_photos if n not in existing or existing.get(n, {}).get("error")]
    progress(f"  ocr: {len(text_photos)} text-bearing photos, {len(pending)} pending")

    if not pending:
        return

    def find_photo(name: str) -> Path | None:
        for p in find_media(data_dir, PHOTO_EXTS):
            if rel_key(data_dir, p) == name or p.name == name:
                return p
        return None

    def ocr_photo(name: str):
        photo = find_photo(name)
        if not photo:
            return name, {"error": "file not found"}
        uri = file_to_data_uri(photo)
        result = client.call("cloud_vlm", {
            "image": uri,
            "prompt": "Transcribe ALL text visible in this image. Include every word, number, and label exactly as shown. Output only the raw transcribed text, nothing else.",
        }, timeout=30)
        text = result.get("response") or result.get("text") or result.get("ocr") or ""
        return name, {"text": text, "error": result.get("error", "")}

    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as pool:
        futures = {pool.submit(ocr_photo, n): n for n in pending}
        done_count = 0
        for fut in as_completed(futures):
            name, info = fut.result()
            existing[name] = info
            done_count += 1
            if done_count % 25 == 0:
                save_json(out, existing)
                progress(f"    {done_count}/{len(pending)} OCR'd")
    save_json(out, existing)
    ok = sum(1 for v in existing.values() if v.get("text"))
    progress(f"  ocr: {ok}/{len(existing)} with extracted text")


def step_objects(client: MCPClient, data_dir: Path, run_dir: Path):
    """Step 12: Object detection on photos with people (YOLOv8)."""
    cls_path = run_dir / "image_classification.json"
    out = run_dir / "object_detection.json"
    classification = load_json(cls_path) or {}
    existing = load_json(out) or {}

    people_photos = [name for name, info in classification.items()
                     if info.get("category") in ("photo_people", "object")]
    pending = [n for n in people_photos if n not in existing]
    progress(f"  objects: {len(people_photos)} photos, {len(pending)} pending")

    if not pending:
        return

    def detect(name: str):
        for p in find_media(data_dir, PHOTO_EXTS):
            if rel_key(data_dir, p) == name or p.name == name:
                uri = file_to_data_uri(p)
                result = client.call("detect_objects", {"imageSource": uri}, timeout=30)
                return name, {"objects": result.get("detections") or result.get("objects") or [],
                              "error": result.get("error", "")}
        return name, {"error": "not found"}

    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as pool:
        futures = {pool.submit(detect, n): n for n in pending}
        done_count = 0
        for fut in as_completed(futures):
            name, info = fut.result()
            existing[name] = info
            done_count += 1
            if done_count % 25 == 0:
                save_json(out, existing)
                progress(f"    {done_count}/{len(pending)} detected")
    save_json(out, existing)
    progress(f"  objects: {len(existing)} photos analyzed")


def step_scenes(client: MCPClient, server: FileServer, data_dir: Path, run_dir: Path):
    """Step 13: Scene detection on videos (PySceneDetect)."""
    out = run_dir / "video_scenes.json"
    existing = load_json(out) or {}
    videos = find_media(data_dir, VIDEO_EXTS)
    pending = [v for v in videos if rel_key(data_dir, v) not in existing]
    progress(f"  scenes: {len(videos)} videos, {len(pending)} pending")

    if not pending:
        return

    for v in pending:
        url = server.url_for(str(v))
        result = client.call("detect_scenes", {"videoSource": url}, timeout=120)
        vkey = rel_key(data_dir, v)
        if "error" not in result:
            existing[vkey] = {"scenes": result.get("scenes", [])}
        else:
            existing[vkey] = {"error": result["error"]}
    save_json(out, existing)
    progress(f"  scenes: {len(existing)} videos analyzed")


def step_normalize(run_dir: Path):
    """Step 14: Kill ghost URLs in all artifact JSONs."""
    import re
    ghost_re = re.compile(r'https?://(?:172\.\d+|localhost|127\.0\.0\.1|0\.0\.0\.0):\d+/')
    fixed = 0
    for jf in run_dir.glob("*.json"):
        try:
            raw = jf.read_text()
            if ghost_re.search(raw):
                cleaned = ghost_re.sub("", raw)
                jf.write_text(cleaned)
                fixed += 1
                progress(f"    {jf.name}: ghost URLs normalized")
        except Exception:
            pass
    progress(f"  normalize: {fixed} files cleaned")


def step_load_db(run_dir: Path):
    """Step 15: Load pre-processed data into SurrealDB."""
    items_data = load_json(run_dir / "items.json")
    if not items_data:
        progress("  load_db: no items.json, skip")
        return

    items = items_data.get("items", [])
    progress(f"  load_db: {len(items)} items to load")

    def esc(s):
        """Escape a string for SurrealQL single-quoted context."""
        return str(s or "").replace("\\", "\\\\").replace("'", "\\'")

    def rid(raw):
        """Build a SurrealDB record-id from a raw string (angle-bracket form
        handles special chars like @, spaces, etc.)."""
        safe = str(raw or "").replace("⟩", "")
        return f"item:⟨{safe}⟩"

    loaded = 0
    errors = 0
    for item in items:
        iid = item.get("id") or hashlib.sha256(
            f"{item.get('type','')}{item.get('path','')}{item.get('timestamp','')}".encode()
        ).hexdigest()[:16]
        sql = (
            f"UPSERT {rid(iid)} SET "
            f"type='{esc(item.get('type'))}', "
            f"path='{esc(item.get('path'))}', "
            f"sender='{esc(item.get('sender'))}', "
            f"timestamp='{esc(item.get('timestamp'))}', "
            f"text='{esc((item.get('text') or '')[:500])}';"
        )
        try:
            surreal_query(sql)
            loaded += 1
        except Exception as e:
            errors += 1
            if errors <= 3:
                progress(f"    DB error: {str(e)[:80]}")
    progress(f"  load_db: {loaded}/{len(items)} items loaded ({errors} errors)")


# ─── Main ────────────────────────────────────────────────────────────────────

STEPS = [
    ("parse",         lambda c,s,d,r: step_parse(d, r)),
    ("faces",         lambda c,s,d,r: step_faces(c, d, r)),
    ("face_clusters", lambda c,s,d,r: step_face_clusters(c, r)),
    ("voice",         lambda c,s,d,r: step_voice(c, s, d, r)),
    ("voice_clusters",lambda c,s,d,r: step_voice_clusters(c, r)),
    ("asr",           lambda c,s,d,r: step_asr(c, s, d, r)),
    ("diarize",       lambda c,s,d,r: step_diarize(c, s, d, r)),
    ("keyframes",     lambda c,s,d,r: step_keyframes(d, r)),
    ("vlm_frames",    lambda c,s,d,r: step_vlm_frames(c, r)),
    ("classify",      lambda c,s,d,r: step_classify(c, d, r)),
    ("ocr",           lambda c,s,d,r: step_ocr(c, d, r)),
    ("objects",       lambda c,s,d,r: step_objects(c, d, r)),
    ("scenes",        lambda c,s,d,r: step_scenes(c, s, d, r)),
    ("normalize",     lambda c,s,d,r: step_normalize(r)),
    ("load_db",       lambda c,s,d,r: step_load_db(r)),
]


def main():
    global MAX_WORKERS
    ap = argparse.ArgumentParser(description="Deterministic multimodal pre-processing pipeline")
    ap.add_argument("--data", help="Path to data directory (e.g., data/ChatExport)")
    ap.add_argument("--run-dir", help="Output directory for artifacts")
    ap.add_argument("--step", help="Run only one step (name from --list)")
    ap.add_argument("--list", action="store_true", help="List available steps")
    ap.add_argument("--workers", type=int, default=MAX_WORKERS, help="Parallel workers")
    args = ap.parse_args()

    if args.list:
        for name, _ in STEPS:
            print(f"  {name}")
        return

    MAX_WORKERS = args.workers

    if not args.data or not args.run_dir:
        ap.error("--data and --run-dir are required (unless --list)")
    data_dir = Path(args.data).resolve()
    run_dir = Path(args.run_dir).resolve()
    run_dir.mkdir(parents=True, exist_ok=True)

    print(f"\n{'='*60}")
    print(f"  Pre-processing Pipeline")
    print(f"  Data:   {data_dir}")
    print(f"  Output: {run_dir}")
    print(f"  Workers: {MAX_WORKERS}")
    print(f"  MCP:    {MCP_URL}")
    print(f"{'='*60}\n")

    client = MCPClient(MCP_URL)
    server = FileServer(str(data_dir))

    steps_to_run = STEPS
    if args.step:
        steps_to_run = [(n, fn) for n, fn in STEPS if n == args.step]
        if not steps_to_run:
            print(f"Unknown step: {args.step}. Use --list to see options.")
            sys.exit(1)

    t_start = time.time()
    for name, fn in steps_to_run:
        run_step(name, fn, client, server, data_dir, run_dir)
        print()

    server.shutdown()
    total = time.time() - t_start
    print(f"{'='*60}")
    print(f"  Pipeline complete in {total:.0f}s")
    print(f"  Artifacts in: {run_dir}")
    print(f"{'='*60}")


if __name__ == "__main__":
    main()
