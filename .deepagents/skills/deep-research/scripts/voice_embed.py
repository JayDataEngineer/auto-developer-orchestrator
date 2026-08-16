#!/usr/bin/env python3
"""Embed every audio file's voice via resemblyzer, cluster, write voice_clusters.json.

Resemblyzer's VoiceEncoder is a WeSpeaker-style ResNet trained on VoxCeleb.
256-d L2-normalized embeddings. Open weights (not gated) — works on a fresh
git clone with the deps in sandbox/Dockerfile.

Output (same shape as face_clusters.json):
    {"labels": [int, ...], "sources": ["file.wav", ...], "model": "resemblyzer", ...}
"""
import json
import os
from pathlib import Path

import numpy as np
from resemblyzer import VoiceEncoder
import soundfile as sf
import librosa
from sklearn.cluster import HDBSCAN

AUDIO_DIR = Path(os.environ.get("AUDIO_DIR",
    "data/telegram-dump/ChatExport_2026-03-13/extracted_audio"))
RUN_DIR = Path(os.environ.get("RUN_DIR",
    "artifacts/run-2026-07-12"))

_encoder = None
def encoder():
    global _encoder
    if _encoder is None:
        _encoder = VoiceEncoder(device="cpu", verbose=False)
    return _encoder


def embed_file(path):
    """Read audio, resample to 16kHz mono, embed. Returns 256-d vector or None."""
    wav, sr = sf.read(str(path))
    # Mono
    if wav.ndim > 1:
        wav = wav[:, 0]
    wav = wav.astype(np.float32)
    # Resample to 16kHz (resemblyzer requirement)
    if sr != 16000:
        wav = librosa.resample(wav, orig_sr=sr, target_sr=16000)
    # Need at least ~1.6s of audio for a meaningful embedding
    if len(wav) < 16000 * 1.6:
        return None
    # Take up to 30s (resemblyzer slides a window; longer = better)
    wav = wav[:16000 * 30]
    return encoder().embed_utterance(wav)


def main():
    files = sorted(AUDIO_DIR.glob("*.wav"))
    # Also include m4a/mp3 if present
    files += sorted(AUDIO_DIR.glob("*.m4a")) + sorted(AUDIO_DIR.glob("*.mp3"))
    print(f"found {len(files)} audio files in {AUDIO_DIR}")
    vectors, sources = [], []
    for i, f in enumerate(files):
        try:
            v = embed_file(f)
            if v is None:
                print(f"  [{i+1}/{len(files)}] {f.name}: too short, skipped", flush=True)
                continue
            vectors.append(v)
            sources.append(f.name)
            if (i + 1) % 10 == 0 or i == 0:
                print(f"  [{i+1}/{len(files)}] {f.name}: dim={len(v)}", flush=True)
        except Exception as e:
            print(f"  [{i+1}/{len(files)}] {f.name}: FAILED {e}", flush=True)
    if not vectors:
        print("no embeddings — aborting")
        return
    print(f"embedded {len(vectors)}/{len(files)} files; clustering...", flush=True)

    X = np.array(vectors)
    # HDBSCAN with cosine. min_cluster_size=2 (small corpus).
    cl = HDBSCAN(min_cluster_size=2, min_samples=1, metric="cosine")
    labels = cl.fit_predict(X).tolist()
    n_clusters = len(set(l for l in labels if l != -1))
    n_noise = labels.count(-1)

    out = {
        "labels": labels,
        "sources": sources,
        "n_items": len(vectors),
        "n_clusters": n_clusters,
        "n_noise": n_noise,
        "model": "resemblyzer-voxceleb-resnet34",
        "dim": int(X.shape[1]),
    }
    out_path = RUN_DIR / "voice_clusters.json"
    out_path.write_text(json.dumps(out, indent=2))
    print(f"wrote {out_path}")
    print(f"  clusters: {n_clusters}  noise: {n_noise}  (of {len(vectors)} embeddings)")
    # Per-cluster summary
    from collections import Counter
    sizes = Counter(labels)
    for cid, n in sorted(sizes.items()):
        label = "noise" if cid == -1 else f"cluster_{cid}"
        print(f"    {label}: {n} files")


if __name__ == "__main__":
    main()
