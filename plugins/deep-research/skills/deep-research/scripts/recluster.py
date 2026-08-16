#!/usr/bin/env python3
"""Re-cluster face/voice embeddings at IDENTITY level.

The original HDBSCAN pass used min_cluster_size=2, which fragments each
person into many tiny clusters (one per near-duplicate frame). That is
useless for identity resolution: Grady ends up with 37 voice clusters
instead of 1.

This script re-runs clustering with agglomerative linkage + cosine
distance threshold — the standard for biometric identity clustering.
Same-person embeddings (cosine similarity > ~0.5 for faces, > ~0.7 for
voice) merge into a single cluster per identity.

Reads:  RUN_DIR/face_embeddings.json, RUN_DIR/voice_embeddings.json
Writes: RUN_DIR/face_clusters.json, RUN_DIR/voice_clusters.json (overwrites)

Output format is identical to the original HDBSCAN output so downstream
ingestion (pipeline_ingest.py, build_entity_dossiers.py) works unchanged.
"""
import json
import sys
from pathlib import Path
import numpy as np
from sklearn.cluster import AgglomerativeClustering


def cluster_embeddings(embeddings, threshold, min_cluster_size=2):
    """Agglomerative cosine clustering. Returns labels array (np.int).
    -1 = noise (singletons / tiny groups below min_cluster_size).
    Cluster IDs are renumbered contiguously by descending size so cluster 0
    is always the largest identity.
    """
    X = np.asarray(embeddings, dtype=np.float32)
    if len(X) == 0:
        return np.array([], dtype=np.int64), {}
    # Normalize for cosine (makes euclidean ~= cosine). We use metric='cosine'
    # directly which AgglomerativeClustering supports.
    ac = AgglomerativeClustering(
        n_clusters=None,
        metric="cosine",
        linkage="average",
        distance_threshold=threshold,
    )
    raw = ac.fit_predict(X)

    # Renumber by descending cluster size, dropping tiny groups as noise
    counts = {}
    for lab in raw:
        counts[int(lab)] = counts.get(int(lab), 0) + 1
    order = sorted(counts, key=lambda c: -counts[c])
    remap = {}
    next_id = 0
    for c in order:
        if counts[c] >= min_cluster_size:
            remap[c] = next_id
            next_id += 1
    labels = np.array(
        [remap.get(int(c), -1) for c in raw], dtype=np.int64)
    return labels, remap


def build_output(labels, metadata, model_name):
    """Build the clusters.json dict in the original HDBSCAN format.

    Includes a `sources` field (one entry per embedding, parallel to `labels`)
    so downstream code (resolve_identities.link_video_audio_to_voice_clusters)
    can map each embedding back to its source file. Built from the embeddings
    metadata: voice → metadata[i]['audio'], face → metadata[i]['photo'].
    """
    n = len(labels)
    members = {}
    for i, lab in enumerate(labels):
        lab_s = str(int(lab))
        members.setdefault(lab_s, []).append(i)
    # drop noise bucket if present
    members.pop("-1", None)
    n_clusters = len(members)
    n_noise = int(np.sum(labels == -1))
    # Build sources list parallel to labels
    sources = []
    for i in range(n):
        m = metadata[i] if i < len(metadata) else {}
        # Voice embeddings use 'audio' or 'path'; face embeddings use 'photo'
        src = m.get("audio") or m.get("photo") or m.get("path") or ""
        sources.append(src)
    return {
        "success": True,
        "labels": [int(x) for x in labels],
        "n_items": n,
        "n_clusters": n_clusters,
        "n_noise": n_noise,
        "members": members,
        "sources": sources,
        "params": {
            "algorithm": "AgglomerativeClustering",
            "linkage": "average",
            "metric": "cosine",
        },
        "model": model_name,
        "metadata": metadata,
    }


def main():
    run_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(
        "artifacts/run-2026-07-23")
    face_threshold = float(sys.argv[2]) if len(sys.argv) > 2 else 0.50
    voice_threshold = float(sys.argv[3]) if len(sys.argv) > 3 else 0.30

    # ---- FACES ----
    fe_path = run_dir / "face_embeddings.json"
    if fe_path.exists():
        fe = json.loads(fe_path.read_text())
        embs = fe["embeddings"]
        meta = fe["metadata"]
        print(f"[faces] {len(embs)} embeddings, dim {len(embs[0])}")
        labels, _ = cluster_embeddings(embs, face_threshold, min_cluster_size=2)
        n_clusters = len(set(int(x) for x in labels if int(x) != -1))
        sizes = {}
        for x in labels:
            x = int(x)
            if x != -1:
                sizes[x] = sizes.get(x, 0) + 1
        top = sorted(sizes.items(), key=lambda kv: -kv[1])[:12]
        print(f"[faces] threshold={face_threshold} -> {n_clusters} clusters, "
              f"sizes top12={top}")
        out = build_output(labels, meta, "ArcFace-Agglomerative")
        (run_dir / "face_clusters.json").write_text(json.dumps(out, indent=2))
        print(f"[faces] wrote {run_dir / 'face_clusters.json'}")
    else:
        print(f"[faces] SKIP — {fe_path} not found")

    # ---- VOICES ----
    ve_path = run_dir / "voice_embeddings.json"
    if ve_path.exists():
        ve = json.loads(ve_path.read_text())
        embs = ve["embeddings"]
        meta = ve["metadata"]
        print(f"[voice] {len(embs)} embeddings, dim {len(embs[0])}")
        labels, _ = cluster_embeddings(embs, voice_threshold, min_cluster_size=2)
        n_clusters = len(set(int(x) for x in labels if int(x) != -1))
        sizes = {}
        for x in labels:
            x = int(x)
            if x != -1:
                sizes[x] = sizes.get(x, 0) + 1
        top = sorted(sizes.items(), key=lambda kv: -kv[1])[:12]
        print(f"[voice] threshold={voice_threshold} -> {n_clusters} clusters, "
              f"sizes top12={top}")
        out = build_output(labels, meta, "Pyannote-Agglomerative")
        (run_dir / "voice_clusters.json").write_text(json.dumps(out, indent=2))
        print(f"[voice] wrote {run_dir / 'voice_clusters.json'}")
    else:
        print(f"[voice] SKIP — {ve_path} not found")


if __name__ == "__main__":
    main()
