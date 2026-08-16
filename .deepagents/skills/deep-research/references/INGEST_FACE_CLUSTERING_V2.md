# INGEST_FACE_CLUSTERING_V2

Recognize and cluster faces across an image corpus + video keyframes, then write each identity cluster as a `face_appearance` record per source image, plus a tentative `person` node per cluster. v2 replaces v1 — uses media-mcp's ONNX stack (InsightFace buffalo_l) instead of CompreFace. No 5-container Java stack.

This is the **first half** of identity resolution. The second half (cross-modal linking via voice) is `INGEST_MULTIMODAL_PERSONS`.

## When to use

You have:
- A directory of images (photos from a chat export, scraped images, etc.)
- Optional: video files (we'll extract 1fps keyframes)
- All paths accessible to the sandbox

You want to answer "who appears in these images, and where?" — producing `face_appearance` records that the multimodal-worker can later link to voices.

## Tool

`media_embed_faces` — InsightFace buffalo_l (SCRFD-10GF detector + MobileFaceNet-22k recognizer, ~30MB total). One call per image returns:
```json
{
  "success": true,
  "count": 2,
  "embedding_dim": 512,
  "faces": [
    {"bbox": [184, 68, 574, 518], "confidence": 0.93, "embedding": [0.021, -0.035, ...]},
    {"bbox": [...], "confidence": 0.87, "embedding": [...]}
  ],
  "image_size": {"width": 1280, "height": 720}
}
```

Embeddings are L2-normalized 512-d vectors. Cosine similarity > 0.5 typically means same person (lower threshold than CompreFace's 0.9 because MobileFaceNet has a different scale).

For URLs the sandbox can't directly serve (local files), upload via the `upload` tool or run a tiny HTTP server in the sandbox:
```bash
cd /sandbox/workspace && python3 -m http.server 9876 &
# Then reference files as http://localhost:9876/path/to/photo.jpg
```

## Pipeline

### Step 1 — Collect image paths

```bash
# Photos
photos=$(find data/<export_dir>/photos -type f -name "*.jpg" \
    | grep -v thumb | sort)

# Video keyframes — use sandbox/video_frames.py, NOT raw ffmpeg.
# video_frames.py runs scene-detection + temporal fallback (captures every
# cut AND forces a frame every ~5s on single-take videos). Naive fps=1
# misses fast cuts and wastes compute on static scenes.
mkdir -p /sandbox/workspace/video_work
for video in data/<export_dir>/<video_subdir>/*.mp4 \
             data/<export_dir>/<video_subdir>/*.MP4; do
    [ -e "$video" ] || continue
    name=$(basename "$video" | tr 'A-Z' 'a-z' | sed 's/\.mp4$//')
    python3 /sandbox/video_frames.py extract-scenes \
        --video "$video" \
        --output /sandbox/workspace/video_work/"$name"
done
# Keyframes are now at /sandbox/workspace/video_work/<name>/frame_*.png
# with frames.json carrying pts_time + scene_score per frame.
keyframes=$(find /sandbox/workspace/video_work -name 'frame_*.png' | sort)
```

### Step 2 — Embed every face in every image

Iterate the image list, call `embed_faces` per image, accumulate results. Write intermediate JSON so the run is resumable:

```python
# Pseudocode — write as make_script if you need to drive it from a loop
results = []
for image_path in all_images:
    url = serve_via_http(image_path)
    res = media_embed_faces(imageSource=url)
    if res["success"] and res["count"] > 0:
        for face in res["faces"]:
            results.append({
                "image": image_path,
                "bbox": face["bbox"],
                "confidence": face["confidence"],
                "embedding": face["embedding"],
            })

# Save
with open("/sandbox/workspace/face_embeddings.json", "w") as f:
    json.dump(results, f)
```

Expected output shape: a flat list of `{image, bbox, confidence, embedding[512]}` records. No grouping yet — clustering is the next step.

### Step 3 — Cluster embeddings into identity groups

```bash
# Extract just the embedding vectors into a JSON array
python3 -c "
import json
data = json.load(open('/sandbox/workspace/face_embeddings.json'))
embeddings = [r['embedding'] for r in data]
print(json.dumps(embeddings))
" > /tmp/face_vectors.json
```

Then call `media_cluster_embeddings` with the embeddings array. Defaults (`min_samples=3`, `min_cluster_size=3`) work for personal-scale corpora. Tune lower if you have <20 faces; higher if you have >500.

Response:
```json
{
  "success": true,
  "n_clusters": 4,
  "n_noise": 7,
  "n_total": 28,
  "labels": [0, 0, 1, 2, -1, 0, 3, ...],
  "cluster_sizes": {"0": 8, "1": 6, "2": 4, "3": 3, "-1": 7},
  "centroids": {
    "0": [0.12, -0.04, ...], "1": [...], "2": [...], "3": [...]
  }
}
```

`labels` is index-aligned with the input `embeddings` array. Label `-1` = noise (couldn't confidently cluster; usually a one-off detection or false positive).

### Step 4 — Write face_appearance records

For each face in `face_embeddings.json` (using its cluster label from step 3), write a SurrealDB record:

```bash
surreal_create(table="face_appearance", data= '{
  "id": "face_001",
  "item_id": "photo_108",
  "image_path": "data/.../photo_108.jpg",
  "bbox": [184, 68, 574, 518],
  "embedding": [0.021, ...],
  "cluster_id": 0,
  "confidence": 0.93
}'
```

For keyframes, also store `frame_sec` (computed from filename pattern `_0001.jpg` → 1 second in):

```bash
surreal_create(table="face_appearance", data= '{
  "id": "face_042",
  "item_id": "video_2",
  "image_path": ".../video_2_0042.jpg",
  "frame_sec": 42,
  "bbox": [...],
  "embedding": [...],
  "cluster_id": 0
}'
```

`frame_sec` is what the multimodal-worker's lip-sync heuristic uses to match against `speaker_turn.start_sec`.

### Step 5 — Write tentative person nodes

One `person` per cluster:

```bash
for cluster_id in 0 1 2 3; do
    surreal_create(table="person", data= "{
      \"id\": \"person_${cluster_id}\",
      \"canonical_name\": null,
      \"face_centroid\": <from cluster centroids output>,
      \"voice_centroid\": null,
      \"face_count\": <from cluster_sizes>,
      \"voice_minutes\": null
    }"
done
```

Don't try to assign canonical names yet — that's a separate supervised step (or the user renames interactively). Leave `canonical_name: null` and identify persons by `person_0`, `person_1`, etc.

### Step 6 — appears_in edges

For each `face_appearance` record, link its person → source item:

```bash
surreal_relate(
    --from person:person_0 --edge appears_in --to item:photo_108 \
    --data '{"role": "photographed", "face_appearance_id": "face_001"}'
```

## Smoke test

```bash
# Single image, verify InsightFace loads + returns embedding
# (replace URL with one served from your sandbox)
curl -sX POST http://localhost:8102/mcp \
    -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"embed_faces","arguments":{"imageSource":"<test_image_url>"}}}'
# Expected: success=true, count>=1, embedding[512]
```

## Pitfalls

1. **Low det_score_threshold default.** `MEDIA_FACES_DET_SCORE_THRESHOLD=0.3` keeps marginal detections. Low-quality source images are — 0.5 misses real faces. If you see many noise points (`-1` cluster label), raise threshold to 0.5 and re-run.
2. **Keyframe extraction rate.** `fps=1` is the default (1 frame per second). For action-heavy video, use `fps=2`. For static talking-head video, `fps=1/2` (one frame every 2 seconds) is enough.
3. **Don't deduplicate embeddings before clustering.** HDBSCAN handles near-duplicates naturally. Pre-deduplication can drop signal.
4. **Centroids are mean of cluster members.** They're NOT the same as a fresh embedding of an "average face." Treat them as identifiers, not as input to a downstream face recognition model.
5. **MobileFaceNet cosine threshold ≈ 0.5, not 0.9.** The 512-d InsightFace vectors are L2-normalized but the metric scale is different from CompreFace's Facenet. Lower threshold accordingly.

## Verification

After running, sanity-check:
```bash
# Total faces embedded
jq 'length' /sandbox/workspace/face_embeddings.json

# Distinct images covered
jq -r '.[].image' /sandbox/workspace/face_embeddings.json | sort -u | wc -l

# Cluster distribution
# (run via cluster_embeddings output inspection)
```

If `length` is 0, the embed_faces calls failed — check media-mcp logs (`docker logs research-media-mcp`).
