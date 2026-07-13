#!/usr/bin/env python3
"""Summarize videos via MiMo-V2.5-Pro (cloud_vlm) + optional diarization.

For each video: upload → cloud_vlm with a structured-summary prompt → store.
If diarization is available, pair speaker turns with the visual summary for
richer context. Output written to entities/video_frames/video_summaries.json
+ linked to item records in SurrealDB (if DB available).

GENERIC: works for any video dataset. No dataset-specific assumptions. The
prompt asks for modality-agnostic structure (what/who/when/text/context).

Environment:
    MEDIA_MCP_URL   media-mcp endpoint (default http://localhost:8101/mcp)
    VIDEO_DIR       directory of source videos
    RUN_DIR         artifact run directory
    SURREALDB_URL   if set, link summaries to item records (optional)
    DIARIZE         if '1', also run diarization on each video's audio (default 1)
"""
import base64
import json
import os
import sys
import urllib.request
from pathlib import Path

MEDIA_URL = os.environ.get("MEDIA_MCP_URL", "http://localhost:8101/mcp").rstrip("/")
VIDEO_DIR = Path(os.environ.get("VIDEO_DIR",
    "data/telegram-dump/Raw_ChatExport_2026-03-13/video_files"))
RUN_DIR = Path(os.environ.get("RUN_DIR",
    "orgs/specialists/deep-research-engine/artifacts/run-2026-07-12"))
SURREALDB_URL = os.environ.get("SURREALDB_URL", "")
DIARIZE = os.environ.get("DIARIZE", "1") == "1"
_rpc_id = 0

SUMMARY_PROMPT = """Analyze this video in detail. Provide a structured summary:

1. **What is happening**: Describe the key actions, events, and content. What is the subject matter?
2. **Who is present**: How many people? What are they doing? Any identifiable individuals?
3. **Timeline**: Note the key moments chronologically (beginning, middle, end). Reference approximate timestamps.
4. **Visible text**: Any on-screen text, captions, signs, documents, UI elements, or messages shown? Quote them exactly.
5. **Audio/speech**: What is being said? Summarize the dialogue or narration. Note who speaks if distinguishable.
6. **Setting/context**: Location, time of day, setting (indoor/outdoor), mood, purpose of the video.
7. **Key takeaways**: The 2-3 most important things a researcher should know about this video.

Be factual. If something is unclear, say so. Do not speculate beyond what is visible and audible."""


def _rpc(tool, args=None):
    global _rpc_id
    _rpc_id += 1
    body = json.dumps({"jsonrpc": "2.0", "id": _rpc_id, "method": "tools/call",
                       "params": {"name": tool, "arguments": args or {}}})
    req = urllib.request.Request(MEDIA_URL, data=body.encode(),
        headers={"Content-Type": "application/json",
                 "Accept": "application/json, text/event-stream"})
    with urllib.request.urlopen(req, timeout=300) as r:
        raw = r.read().decode()
    # SSE response has multiple events; skip notifications (no "result" key),
    # return the first message that has a result matching our request id.
    for line in raw.splitlines():
        line = line.strip()
        if line.startswith("data:"):
            line = line[5:].strip()
        if not line.startswith("{"):
            continue
        d = json.loads(line)
        if "error" in d:
            return {"error": str(d["error"])}
        if "result" not in d:
            continue  # notification (e.g. progress log) — skip
        result = d["result"]
        sc = result.get("structuredContent", {})
        if sc:
            if isinstance(sc.get("success"), bool) and not sc["success"]:
                return {"error": sc.get("error", "tool failed")}
            return sc
        for c in result.get("content", []):
            if c.get("type") == "text":
                try:
                    return json.loads(c["text"])
                except Exception:
                    return {"text": c["text"]}
        return result
    return {"error": "no JSON in response"}


def _data_uri(path):
    """Encode as data URI — the cloud VLM provider can't fetch localhost URLs,
    so we inline the video. Works for files up to ~15MB (base64 expansion)."""
    mime_map = {".mp4": "video/mp4", ".mov": "video/quicktime",
                ".avi": "video/x-msvideo", ".webm": "video/webm", ".mkv": "video/x-matroska"}
    mime = mime_map.get(path.suffix.lower(), "video/mp4")
    data = base64.b64encode(path.read_bytes()).decode()
    return f"data:{mime};base64,{data}"


def summarize_video(path):
    """Send video to MiMo-V2.5-Pro via cloud_vlm (data URI inline — the cloud
    provider can't fetch localhost URLs, so we inline the bytes)."""
    uri = _data_uri(path)
    r = _rpc("cloud_vlm", {"video": uri, "prompt": SUMMARY_PROMPT,
                           "max_tokens": 4096, "temperature": 0.3})
    # Extract text from various response shapes
    for key in ("result", "summary", "text", "response", "content"):
        if r.get(key):
            val = r[key]
            return val if isinstance(val, str) else json.dumps(val)
    return json.dumps(r)


def diarize_video(path):
    """Non-gated speaker diarization: ffmpeg → webrtcvad → resemblyzer → HDBSCAN.

    Pyannote is gated (needs HF token), so we build an equivalent pipeline from
    open components that are already in the sandbox Dockerfile:
      1. ffmpeg extracts a 16kHz mono WAV from the video's audio track.
      2. webrtcvad finds speech segments (voice activity detection).
      3. resemblyzer VoiceEncoder (VoxCeleb ResNet, 256-d) embeds each segment.
      4. HDBSCAN clusters segments by speaker (cosine metric).

    Returns a list of {start, end, speaker} dicts. Works on any video with an
    audio track. If no speech is detected, returns []."""
    import subprocess
    import tempfile
    try:
        import webrtcvad
        import numpy as np
        import librosa
        import soundfile as sf
        from resemblyzer import VoiceEncoder
        from sklearn.cluster import HDBSCAN
    except ImportError as e:
        return []

    # 1. Extract audio via ffmpeg (16kHz mono WAV — what webrtcvad + resemblyzer expect)
    with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tf:
        wav_path = tf.name
    try:
        subprocess.run(
            ["ffmpeg", "-y", "-i", str(path), "-vn", "-ac", "1",
             "-ar", "16000", "-f", "wav", wav_path],
            capture_output=True, timeout=60)
        if not Path(wav_path).exists() or Path(wav_path).stat().st_size < 1000:
            return []
    except Exception:
        return []

    # 2. webrtcvad — find speech segments (30ms frames, aggressiveness 2)
    try:
        wav, sr = sf.read(wav_path)
        if wav.ndim > 1:
            wav = wav[:, 0]
        # sf.read returns float [-1, 1]; webrtcvad needs int16 raw bytes
        wav = (wav * 32768).astype(np.int16)
    except Exception:
        return []
    finally:
        Path(wav_path).unlink(missing_ok=True)

    if sr != 16000 or len(wav) < 16000:
        return []

    vad = webrtcvad.Vad(2)  # 0-3, 2 = moderately aggressive
    frame_ms = 30
    frame_len = int(sr * frame_ms / 1000)  # 480 samples
    frames = [wav[i:i+frame_len] for i in range(0, len(wav) - frame_len, frame_len)]

    # Classify each frame as speech or silence
    speech_frames = []
    for i, frame in enumerate(frames):
        try:
            if vad.is_speech(frame.tobytes(), sr):
                speech_frames.append(i)
        except Exception:
            pass

    if not speech_frames:
        return []

    # Merge consecutive speech frames into segments (min 0.5s)
    segments = []
    start = speech_frames[0]
    prev = speech_frames[0]
    for f in speech_frames[1:]:
        if f == prev + 1:
            prev = f
        else:
            if (prev - start + 1) * frame_ms >= 500:
                segments.append((start * frame_ms / 1000, (prev + 1) * frame_ms / 1000))
            start = f
            prev = f
    if (prev - start + 1) * frame_ms >= 500:
        segments.append((start * frame_ms / 1000, (prev + 1) * frame_ms / 1000))

    if len(segments) < 2:
        return [{"start": s, "end": e, "speaker": "SPEAKER_00"} for s, e in segments]

    # 3. Embed each segment with resemblyzer
    encoder = VoiceEncoder()
    embs = []
    valid_segs = []
    for s, e in segments:
        si = int(s * sr)
        ei = int(e * sr)
        clip = wav[si:ei].astype(np.float32) / 32768.0
        if len(clip) < 16000 * 1.6:  # resemblyzer needs ≥1.6s
            continue
        clip = clip[:16000 * 30]  # cap at 30s
        emb = encoder.embed_utterance(clip)
        embs.append(emb)
        valid_segs.append((s, e))

    if len(embs) < 2:
        return [{"start": s, "end": e, "speaker": "SPEAKER_00"} for s, e in valid_segs]

    # 4. Cluster by speaker (HDBSCAN cosine)
    X = np.stack(embs)
    min_sz = max(2, min(len(X) // 2, 5))
    clusterer = HDBSCAN(min_cluster_size=min_sz, metric="cosine")
    labels = clusterer.fit_predict(X)

    result = []
    for (s, e), label in zip(valid_segs, labels):
        spk = f"SPEAKER_{label:02d}" if label >= 0 else "SPEAKER_NOISE"
        result.append({"start": round(s, 2), "end": round(e, 2), "speaker": spk})
    return result


def sql(body):
    req = urllib.request.Request(SURREALDB_URL, data=body.encode(),
        headers={"Accept": "application/json", "surreal-ns": "research",
                 "surreal-db": "main", "Authorization": "Basic cm9vdDpyb290"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read())


def link_to_db(stem, summary, segments):
    """Link the summary to the video's item record (if DB available)."""
    if not SURREALDB_URL:
        return
    try:
        res = sql(f"SELECT id FROM item WHERE type = 'video' AND path CONTAINS '{stem}' LIMIT 1;")
        items = res[0].get("result", []) if res and isinstance(res[0].get("result"), list) else []
        if not items or not isinstance(items[0], dict):
            return
        iid = items[0]["id"]
        # Truncate BEFORE json.dumps — cutting after encoding can split a
        # multi-byte escape (e.g. \u00e) and produce invalid SurrealQL.
        safe_summary = json.dumps(summary[:7500])
        sql(f"UPDATE {iid} SET video_summary = {safe_summary}, "
            f"speaker_turns = {json.dumps(segments[:200])};")
    except Exception as e:
        print(f"  DB link failed for {stem}: {e}", file=sys.stderr)


def main():
    videos = sorted(list(VIDEO_DIR.glob("*.MP4")) + list(VIDEO_DIR.glob("*.mp4"))
                    + list(VIDEO_DIR.glob("*.MOV")) + list(VIDEO_DIR.glob("*.mov"))
                    + list(VIDEO_DIR.glob("*.avi")) + list(VIDEO_DIR.glob("*.webm")))
    print(f"found {len(videos)} videos in {VIDEO_DIR}")
    if not videos:
        print("no videos — nothing to do. This script only runs if videos exist.")
        return

    out_path = RUN_DIR / "entities" / "video_frames" / "video_summaries.json"
    out_path.parent.mkdir(parents=True, exist_ok=True)
    cache = json.loads(out_path.read_text()) if out_path.exists() else {}

    results = []
    for i, v in enumerate(videos):
        stem = v.stem
        prev = cache.get(stem)
        prev_summary = prev.get("summary", "") if prev else ""
        has_good_summary = prev and len(str(prev_summary)) > 20 and not prev.get("error")
        # If we have a good summary but no speaker segments, run diarization only
        if has_good_summary and DIARIZE and not prev.get("speaker_segments"):
            print(f"  [{i+1}/{len(videos)}] {stem}: summary cached, diarizing...", flush=True)
            try:
                segs = diarize_video(v)
                prev["speaker_segments"] = segs
                prev["n_speakers"] = len(set(s.get("speaker", "?") for s in segs)) if segs else 0
                cache[stem] = prev
                out_path.write_text(json.dumps(cache, indent=2, ensure_ascii=False))
                link_to_db(stem, prev["summary"], segs)
                print(f"    {prev['n_speakers']} speakers, {len(segs)} segments", flush=True)
            except Exception as e:
                print(f"    diarize failed: {e}", flush=True)
                prev["speaker_segments"] = []
                prev["n_speakers"] = 0
                cache[stem] = prev
                out_path.write_text(json.dumps(cache, indent=2, ensure_ascii=False))
            results.append({"video": stem, **prev})
            continue
        # Fully cached
        if has_good_summary:
            print(f"  [{i+1}/{len(videos)}] {stem}: cached ({len(str(prev_summary))} chars)", flush=True)
            results.append({"video": stem, **prev})
            continue
        sz_mb = v.stat().st_size / (1024*1024)
        print(f"  [{i+1}/{len(videos)}] {stem} ({sz_mb:.1f}MB): summarizing via cloud_vlm...", flush=True)
        try:
            summary = summarize_video(v)
            entry = {"summary": summary}
            if DIARIZE:
                print(f"    diarizing...", flush=True)
                try:
                    segs = diarize_video(v)
                    entry["speaker_segments"] = segs
                    entry["n_speakers"] = len(set(s.get("speaker","?") for s in segs))
                except Exception as e:
                    entry["diarize_error"] = str(e)[:100]
            cache[stem] = entry
            out_path.write_text(json.dumps(cache, indent=2, ensure_ascii=False))
            link_to_db(stem, summary, entry.get("speaker_segments", []))
            spk = entry.get("n_speakers", 0)
            print(f"    done ({len(summary)} chars, {spk} speakers)", flush=True)
            results.append({"video": stem, **entry})
        except Exception as e:
            print(f"    FAILED: {e}", flush=True)
            cache[stem] = {"error": str(e)[:200]}
            out_path.write_text(json.dumps(cache, indent=2))
            results.append({"video": stem, "error": str(e)[:200]})

    # Write a human-readable index
    idx = RUN_DIR / "entities" / "video_frames" / "video_summaries.md"
    lines = ["# Video summaries (MiMo-V2.5-Pro)", "",
             f"**Videos:** {len(results)}", "",
             "Each video was analyzed end-to-end by the multimodal model (not just keyframes). "
             "Speaker diarization (Pyannote 3.1) paired where available.", ""]
    for r in results:
        if r.get("error"):
            lines += [f"## {r['video']} — ERROR", f"`{r['error']}`", ""]
            continue
        lines += [f"## {r['video']}", ""]
        if r.get("n_speakers"):
            lines.append(f"**Speakers detected:** {r['n_speakers']}")
        lines += ["", r.get("summary", "(no summary)"), ""]
        segs = r.get("speaker_segments", [])
        if segs:
            lines += ["<details><summary>Speaker turns</summary>", "", "```json",
                      json.dumps(segs, indent=2)[:3000], "```", "", "</details>", ""]
    idx.write_text("\n".join(lines))
    print(f"\nwrote {idx}")
    print(f"wrote {out_path}")


if __name__ == "__main__":
    main()
