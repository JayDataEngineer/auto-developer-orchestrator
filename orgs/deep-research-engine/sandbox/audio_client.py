#!/usr/bin/env python3
"""Audio transcription + speaker diarization client for Pux sandbox workers.

Wraps the local media-analysis-mcp container's HTTP API. Two stages:
  1. transcribe — Parakeet TDT v3 (word-level timestamps + punctuation)
  2. diarize    — Pyannote 3.1 (speaker segments, globally stable IDs)

After both run, segments are aligned by timestamp overlap: each transcript
chunk gets the speaker whose diarization window covers its start time.

Usage:
    # One-shot — transcribe + diarize + align
    python3 audio_client.py process --audio voice.ogg --output turns.json

    # Just transcript (Parakeet)
    python3 audio_client.py transcribe --audio voice.ogg

    # Just diarization (Pyannote)
    python3 audio_client.py diarize --audio voice.ogg

    # Batch — process every voice file in a directory
    python3 audio_client.py batch --input voice_messages/ --output turns/

Environment:
    MEDIA_MCP_URL    Base URL of the media MCP container.
                     Default: http://localhost:8102 (research-media-mcp via host port)
                     Inside the same docker network: http://media-mcp:8001
    AUDIO_HTTP_BASE  Base URL the MCP can fetch audio from. Set this if your
                     audio is on a host path; the script starts a throwaway
                     HTTP server bound to 0.0.0.0 and uses the docker bridge IP
                     (or Tailscale IP if AUDIO_HTTP_PUBLIC is set) to expose it.

Output schema (turns.json):
    {
      "audio": "voice.ogg",
      "duration_sec": 12.34,
      "transcript": "Hello, this is a test.",
      "speakers": ["speaker_0", "speaker_1"],
      "turns": [
        {
          "speaker": "speaker_0",
          "start": 0.0,
          "end": 4.21,
          "text": "Hello, this is a test."
        }
      ]
    }

Auditor contract: voice message MUST have non-empty transcript AND ≥1 speaker
label. Empty transcript or zero turns is a silent failure — flag it.
"""

import argparse
import contextlib
import http.server
import json
import os
import socket
import socketserver
import sys
import threading
import time
import urllib.parse
import urllib.request
from pathlib import Path


# ---------------------------------------------------------------------------
# Config

def get_mcp_url():
    return os.environ.get("MEDIA_MCP_URL", "http://localhost:8102").rstrip("/")


def get_audio_base():
    """URL the MCP container can fetch audio from. Empty = spawn one-shot server."""
    return os.environ.get("AUDIO_HTTP_BASE", "").rstrip("/")


# ---------------------------------------------------------------------------
# One-shot HTTP server so the MCP can fetch a local file

class _QuietHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args, **kwargs):
        pass  # silence stderr logging


@contextlib.contextmanager
def _serve_path(path: Path):
    """Serve `path` (file or dir) over HTTP on a random port. Yields the URL."""
    path = path.resolve()
    serve_dir = path if path.is_dir() else path.parent
    cwd = os.getcwd()
    os.chdir(serve_dir)
    httpd = socketserver.TCPServer(("0.0.0.0", 0), _QuietHandler)
    port = httpd.server_address[1]
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    try:
        # Prefer Tailscale IP if available — MCP container can reach it
        public_ip = os.environ.get("AUDIO_HTTP_PUBLIC", "")
        if not public_ip:
            # Try to detect Tailscale IP
            try:
                s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
                s.connect(("100.86.69.57", 53))  # any tailscale addr — won't actually send
                # actually this won't work for our container; use the docker bridge
                s.close()
            except Exception:
                pass
            # Default: docker bridge gateway IP — container reaches host via 172.17.0.1
            # Override by setting AUDIO_HTTP_PUBLIC=<tailscale-ip> if running on a host
            # whose MCP container is on a custom network.
            public_ip = os.environ.get("AUDIO_HTTP_PUBLIC", "172.17.0.1")
        suffix = path.name if path.is_file() else ""
        url = f"http://{public_ip}:{port}/{urllib.parse.quote(suffix)}"
        yield url
    finally:
        httpd.shutdown()
        httpd.server_close()
        thread.join(timeout=2)
        os.chdir(cwd)


# ---------------------------------------------------------------------------
# MCP HTTP calls

def _wait_for_mcp(max_seconds=120):
    """Block until MCP health endpoint responds. Prints progress."""
    url = f"{get_mcp_url()}/health"
    deadline = time.time() + max_seconds
    last_err = ""
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=5) as resp:
                if resp.status == 200:
                    return True
        except Exception as e:
            last_err = str(e)
        time.sleep(2)
    raise RuntimeError(f"MCP not healthy at {url} within {max_seconds}s (last error: {last_err})")


def _mcp_call(tool_name: str, arguments: dict, timeout=300) -> dict:
    """Call an MCP tool via JSON-RPC over HTTP POST.

    The slim media-mcp exposes tools via the MCP protocol at the /mcp endpoint
    (not as individual REST endpoints).  This helper wraps the JSON-RPC call.

    Returns the tool's result dict (parsed from content[0].text).
    """
    import requests as _requests

    url = f"{get_mcp_url()}/mcp"
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": tool_name,
            "arguments": arguments,
        },
    }
    # Use streaming to read SSE events line by line
    resp = _requests.post(
        url,
        json=payload,
        headers={"Accept": "application/json, text/event-stream"},
        timeout=timeout,
        stream=True,
    )
    resp.raise_for_status()

    # Read SSE events until we get a "message" event with data
    event_type = None
    event_data = ""
    for line in resp.iter_lines(decode_unicode=True):
        if line is None:
            continue
        line = line.strip()
        if line.startswith("event: "):
            event_type = line[7:]
        elif line.startswith("data: "):
            event_data = line[6:]
        elif line == "":
            # End of event — if it's a message, parse it
            if event_type == "message" and event_data:
                rpc_resp = json.loads(event_data)
                break
            event_type = None
            event_data = ""

    resp.close()

    if not event_data:
        raise RuntimeError(f"MCP response had no data events (got {event_type})")

    if "error" in rpc_resp:
        raise RuntimeError(f"MCP error: {rpc_resp['error']}")

    # MCP result wraps tool output in content array
    result = rpc_resp.get("result", {})
    content = result.get("content", [])
    if content and content[0].get("type") == "text":
        inner = json.loads(content[0]["text"])
        return inner

    return result


def transcribe_audio(audio_url, timeout=600):
    """Call transcribe_audio tool via MCP protocol."""
    return _mcp_call("transcribe_audio", {"audioSource": audio_url}, timeout=timeout)


def diarize_audio(audio_url, num_speakers=None, timeout=600):
    """Call diarize_audio tool via MCP protocol."""
    args = {"audioSource": audio_url}
    if num_speakers is not None:
        args["num_speakers"] = num_speakers
    return _mcp_call("diarize_audio", args, timeout=timeout)


# ---------------------------------------------------------------------------
# Alignment: assign each transcript chunk to a speaker

def _transcript_chunks(transcript_resp):
    """Extract (text, start, end) chunks from a transcribe response.

    Parakeet TDT (via onnx-asr with_timestamps) returns token-level timestamps;
    we group into phrase chunks by gap detection (>0.3s silence) AND sentence-
    final punctuation (. ! ?). This gives chunks small enough to align with
    per-speaker diarization windows.
    """
    if isinstance(transcript_resp, dict):
        # New shape: {text, tokens, timestamps}
        if "tokens" in transcript_resp and "timestamps" in transcript_resp:
            tokens = transcript_resp["tokens"] or []
            timestamps = transcript_resp["timestamps"] or []
            if tokens and timestamps and len(tokens) == len(timestamps):
                # Build (token, start, end) tuples
                paired = []
                for i, (tok, start) in enumerate(zip(tokens, timestamps)):
                    end = timestamps[i + 1] if i + 1 < len(timestamps) else start + 0.3
                    paired.append((str(tok), float(start), float(end)))
                # Group by gap or sentence-final punctuation
                SENTENCE_END = {".", "!", "?", "。", "！", "？"}
                chunks = []
                buf = []
                cstart = None
                cend = None
                prev_end = None
                for tok, s, e in paired:
                    flush = (
                        prev_end is not None
                        and (s - prev_end) > 0.3
                        and buf
                    ) or (
                        buf and buf[-1].strip() in SENTENCE_END
                    )
                    if flush:
                        chunks.append(("".join(buf).strip(), cstart, cend))
                        buf = []
                        cstart = None
                    if cstart is None:
                        cstart = s
                    cend = e
                    buf.append(tok)
                    prev_end = e
                if buf:
                    chunks.append(("".join(buf).strip(), cstart, cend))
                return [c for c in chunks if c[0]]
            text = (transcript_resp.get("text") or "").strip()
            return [(text, 0.0, 0.0)] if text else []
        # Legacy shapes
        if "words" in transcript_resp:
            words = transcript_resp["words"]
        elif "segments" in transcript_resp and transcript_resp["segments"]:
            segs = transcript_resp["segments"]
            if isinstance(segs, list) and isinstance(segs[0], dict) and "start" in segs[0]:
                return [
                    (s.get("text", "").strip(), float(s.get("start", 0)), float(s.get("end", 0)))
                    for s in segs
                    if s.get("text")
                ]
            words = segs
        else:
            text = (transcript_resp.get("text") or "").strip()
            return [(text, 0.0, 0.0)] if text else []
    elif isinstance(transcript_resp, str):
        text = transcript_resp.strip()
        return [(text, 0.0, 0.0)] if text else []
    else:
        return []

    chunks = []
    current_words = []
    current_start = None
    current_end = None
    prev_end = None
    for w in words:
        if not isinstance(w, dict):
            continue
        text = (w.get("word") or w.get("text") or "").strip()
        if not text:
            continue
        start = float(w.get("start", w.get("start_time", 0)))
        end = float(w.get("end", w.get("end_time", start + 0.1)))
        if prev_end is not None and (start - prev_end) > 0.5 and current_words:
            chunks.append((" ".join(current_words), current_start, current_end))
            current_words = []
            current_start = None
        if current_start is None:
            current_start = start
        current_end = end
        current_words.append(text)
        prev_end = end
    if current_words:
        chunks.append((" ".join(current_words), current_start, current_end))
    return chunks


def _diarization_segments(diarize_resp):
    """Normalize diarization response into [(start, end, speaker), ...]"""
    if isinstance(diarize_resp, dict):
        segs = diarize_resp.get("segments") or diarize_resp.get("turns") or []
    elif isinstance(diarize_resp, list):
        segs = diarize_resp
    else:
        return []
    out = []
    for s in segs:
        if not isinstance(s, dict):
            continue
        sp = s.get("speaker") or s.get("label") or s.get("speaker_id")
        if sp is None:
            continue
        start = float(s.get("start", s.get("start_time", 0)))
        end = float(s.get("end", s.get("end_time", start + 0.1)))
        if end <= start:
            continue
        out.append((start, end, str(sp)))
    return out


def align(transcript_chunks, diar_segments):
    """Assign each chunk a speaker by timestamp overlap with diarization windows.

    Falls back to the nearest speaker window if no overlap (e.g., chunk has
    no timestamps). Returns list of {speaker, start, end, text} dicts.

    After per-chunk speaker assignment, merges consecutive chunks with the
    same speaker into single turns so the output reads naturally.
    """
    if not diar_segments:
        # No diarization — assign everything to speaker_0
        return [
            {"speaker": "speaker_0", "start": c[1], "end": c[2], "text": c[0]}
            for c in transcript_chunks
        ]

    labeled = []
    for text, c_start, c_end in transcript_chunks:
        if c_start == 0 and c_end == 0:
            sp = diar_segments[0][2]
        else:
            mid = (c_start + c_end) / 2
            sp = None
            for ds, de, dlabel in diar_segments:
                if ds <= mid <= de:
                    sp = dlabel
                    break
            if sp is None:
                nearest = min(diar_segments, key=lambda d: abs(d[0] - mid))
                sp = nearest[2]
        labeled.append({"speaker": sp, "start": c_start, "end": c_end, "text": text})

    # Merge consecutive chunks with the same speaker
    merged = []
    for chunk in labeled:
        if merged and merged[-1]["speaker"] == chunk["speaker"]:
            sep = " " if merged[-1]["text"].endswith((".", "!", "?", "。", "！", "？")) else ""
            merged[-1]["text"] += sep + chunk["text"]
            merged[-1]["end"] = max(merged[-1]["end"], chunk["end"])
        else:
            merged.append(dict(chunk))
    # Strip turn text
    for t in merged:
        t["text"] = t["text"].strip()
    return [t for t in merged if t["text"]]


# ---------------------------------------------------------------------------
# Top-level pipeline

def process_audio(audio_path, num_speakers=None, timeout=900):
    """Transcribe + diarize + align. Returns the full record dict."""
    audio_path = Path(audio_path).resolve()
    if not audio_path.exists():
        raise FileNotFoundError(f"audio not found: {audio_path}")

    # Serve the file so MCP can fetch it
    audio_base = get_audio_base()
    if audio_base:
        audio_url = f"{audio_base}/{urllib.parse.quote(audio_path.name)}"
    else:
        with _serve_path(audio_path) as url:
            audio_url = url
            return _do_process(audio_path, audio_url, num_speakers, timeout)
    return _do_process(audio_path, audio_url, num_speakers, timeout)


def _do_process(audio_path, audio_url, num_speakers, timeout):
    t_resp = transcribe_audio(audio_url, timeout=timeout)
    try:
        d_resp = diarize_audio(audio_url, num_speakers=num_speakers, timeout=timeout)
    except Exception as e:
        # Diarization failure is NOT fatal — we still have a transcript
        sys.stderr.write(f"warning: diarization failed ({e}); returning transcript-only\n")
        d_resp = {"segments": []}

    chunks = _transcript_chunks(t_resp)
    segments = _diarization_segments(d_resp)
    turns = align(chunks, segments)

    speakers = sorted({t["speaker"] for t in turns})
    transcript_text = " ".join(t["text"] for t in turns).strip()

    # Duration estimate
    duration = 0.0
    if segments:
        duration = max(s[1] for s in segments)
    elif chunks:
        duration = max(c[2] for c in chunks)

    return {
        "audio": str(audio_path),
        "duration_sec": round(duration, 3),
        "transcript": transcript_text,
        "speakers": speakers,
        "turns": turns,
        "_raw": {"transcribe": t_resp, "diarize": d_resp},
    }


# ---------------------------------------------------------------------------
# CLI

def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_proc = sub.add_parser("process", help="transcribe + diarize + align")
    p_proc.add_argument("--audio", required=True, help="path to audio file")
    p_proc.add_argument("--output", help="write JSON to this path (default: stdout)")
    p_proc.add_argument("--num-speakers", type=int, default=None)
    p_proc.add_argument("--timeout", type=int, default=900)
    p_proc.add_argument("--wait-for-mcp", action="store_true",
                       help="block up to 120s for MCP to be healthy before sending requests")

    p_t = sub.add_parser("transcribe", help="transcribe only (Parakeet)")
    p_t.add_argument("--audio", required=True)
    p_t.add_argument("--output")
    p_t.add_argument("--timeout", type=int, default=600)
    p_t.add_argument("--wait-for-mcp", action="store_true")

    p_d = sub.add_parser("diarize", help="diarize only (Pyannote)")
    p_d.add_argument("--audio", required=True)
    p_d.add_argument("--output")
    p_d.add_argument("--num-speakers", type=int, default=None)
    p_d.add_argument("--timeout", type=int, default=600)
    p_d.add_argument("--wait-for-mcp", action="store_true")

    p_b = sub.add_parser("batch", help="process every audio file in a directory")
    p_b.add_argument("--input", required=True, help="directory of audio files")
    p_b.add_argument("--output", required=True, help="output directory")
    p_b.add_argument("--num-speakers", type=int, default=None)
    p_b.add_argument("--timeout", type=int, default=900)
    p_b.add_argument("--wait-for-mcp", action="store_true")

    args = ap.parse_args()

    if getattr(args, "wait_for_mcp", False):
        sys.stderr.write("waiting for MCP health...\n")
        _wait_for_mcp()
        sys.stderr.write("MCP healthy\n")

    if args.cmd == "process":
        rec = process_audio(args.audio, num_speakers=args.num_speakers, timeout=args.timeout)
        _emit(rec, args.output)
    elif args.cmd == "transcribe":
        audio_url = _resolve_audio_url(args.audio)
        rec = transcribe_audio(audio_url, timeout=args.timeout)
        _emit(rec, args.output)
    elif args.cmd == "diarize":
        audio_url = _resolve_audio_url(args.audio)
        rec = diarize_audio(audio_url, num_speakers=args.num_speakers, timeout=args.timeout)
        _emit(rec, args.output)
    elif args.cmd == "batch":
        in_dir = Path(args.input)
        out_dir = Path(args.output)
        out_dir.mkdir(parents=True, exist_ok=True)
        exts = {".ogg", ".mp3", ".wav", ".flac", ".m4a", ".opus", ".webm"}
        files = sorted(p for p in in_dir.iterdir() if p.suffix.lower() in exts)
        if not files:
            sys.exit(f"no audio files found in {in_dir}")
        results = []
        for f in files:
            sys.stderr.write(f"processing {f.name}...\n")
            try:
                rec = process_audio(str(f), num_speakers=args.num_speakers, timeout=args.timeout)
                results.append(rec)
                out_file = out_dir / (f.stem + ".json")
                out_file.write_text(json.dumps(rec, indent=2, ensure_ascii=False))
            except Exception as e:
                sys.stderr.write(f"  ERROR: {e}\n")
                results.append({"audio": str(f), "error": str(e)})
        summary = {
            "total": len(files),
            "ok": sum(1 for r in results if "error" not in r),
            "failed": sum(1 for r in results if "error" in r),
            "results": results,
        }
        (out_dir / "_summary.json").write_text(json.dumps(summary, indent=2, ensure_ascii=False))
        sys.stderr.write(f"done: {summary['ok']}/{summary['total']} ok\n")


def _resolve_audio_url(audio_path):
    """Resolve a local path to a URL the MCP can fetch. Used by transcribe/diarize subcommands."""
    audio_path = Path(audio_path).resolve()
    if not audio_path.exists():
        raise FileNotFoundError(f"audio not found: {audio_path}")
    audio_base = get_audio_base()
    if audio_base:
        return f"{audio_base}/{urllib.parse.quote(audio_path.name)}"
    # For subcommands that don't need alignment, we still have to serve the file.
    # Simplest: use the same context-manager pattern via a fresh server.
    # But _http_post_json is sync — we need to start the server, send the request,
    # then stop. Easier: just spawn one server and tear it down manually.
    raise RuntimeError(
        "AUDIO_HTTP_BASE not set. For subcommands 'transcribe' and 'diarize', "
        "either:\n"
        "  1. set AUDIO_HTTP_BASE=http://your-host:port/ and serve the dir yourself, or\n"
        "  2. use the 'process' subcommand which auto-serves the file."
    )


def _emit(rec, output_path):
    text = json.dumps(rec, indent=2, ensure_ascii=False)
    if output_path:
        Path(output_path).write_text(text)
        sys.stderr.write(f"wrote {output_path}\n")
    else:
        print(text)


if __name__ == "__main__":
    main()
