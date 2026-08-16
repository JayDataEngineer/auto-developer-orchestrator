# INGEST_FACE_CLUSTERING

Recognize and cluster faces across an image corpus, then write each identity
cluster as a `person` node + `appears_in` edges to the media where they appear.
This is the identity-resolution half of image ingestion — run after
INGEST_STRUCTURED_EXPORT (or any skill that produces a list of image paths).

## When to use

You have a directory of images (photos from a chat export, scraped images,
video keyframes). You want to answer "who appears in these images, and where?"

Do NOT run this skill until you have:
- The image corpus on disk (file paths accessible to the sandbox).
- CompreFace up and `.env.local` populated (it is, if `./bootstrap.sh` ran).

## Tool

`sandbox/face_client.py` — wraps CompreFace REST API. Six subcommands:

| Subcommand | Purpose |
|------------|---------|
| `recognize --image P --min-similarity 0.8` | Recognize faces in one image |
| `add-subject --name N --image P` | Register a known face under name N |
| `list-subjects` | Show all registered subjects |
| `delete-subject --name N` | Delete a subject (pre-count + DELETE) |
| `batch-recognize --input list.json --output out.json` | Recognize across many images |
| `cluster --input out.json --output clusters.json` | Group face embeddings via HDBSCAN |

The script reads `COMPREFACE_BASE_URL` and `COMPREFACE_API_KEY` from env (or
`.env.local` if you source it first). All HTTP calls go through Caddy on
`http://localhost:8000` — no direct port access needed.

## Pipeline

### Step 1 — Batch recognize (one HTTP call per image)

```bash
set -a && . ./.env.local && set +a

# Build image list (JSON array of paths)
python3 -c "
import json, pathlib
photos = sorted(pathlib.Path('data/<export_dir>/photos').glob('*.jpg'))
photos = [str(p) for p in photos if 'thumb' not in p.name]
print(json.dumps(photos))
" > /tmp/image_list.json

python3 sandbox/face_client.py batch-recognize \
    --input /tmp/image_list.json \
    --output /tmp/batch_out.json \
    --min-similarity 0.0   # keep all faces; cluster step decides identity
```

**Output schema** (`/tmp/batch_out.json`):

```json
[
  {
    "image": "data/.../photo_108.jpg",
    "faces_found": 1,
    "results": [
      {
        "name": "Unknown",              // subject name from CompreFace (Unknown if not registered)
        "confidence": 0.0,              // similarity to matched subject (0 if Unknown)
        "box": {"x_min": 184, "y_min": 68, "x_max": 574, "y_max": 518, "probability": 0.99992},
        "embedding": [0.021, -0.035, ...]   // 128-dim unit vector
      }
    ]
  }
]
```

`embedding` is the key field for clustering — CompreFace's Facenet-style 128-dim
vector. Two embeddings from the same person will be very close in cosine
similarity (>0.9 typically).

### Step 2 — Cluster embeddings into identity groups

```bash
python3 sandbox/face_client.py cluster \
    --input /tmp/batch_out.json \
    --output /tmp/clusters.json \
    --min-cluster-size 2
```

HDBSCAN with euclidean-on-unit-vectors (= cosine similarity). DBSCAN fallback
if `hdbscan` package unavailable. Each face in the input gets a `cluster`
integer and `cluster_label` (`person_0`, `person_1`, ... or `unclustered`
for noise points).

**Tuning:**
- `--min-cluster-size 2` (default): minimum faces per cluster. Raise to 3+ to
  reject one-off misrecognitions.
- For tighter clusters, edit `cluster_selection_epsilon` in `face_client.py`
  (default 0.3). Higher value = more permissive merging.

### Step 3 — Register known persons (optional, supervised)

If you recognize a face and want future recognizes to return their name
instead of "Unknown":

```bash
# Register by name
python3 sandbox/face_client.py add-subject --name "Jane Doe" \
    --image data/.../photo_with_jane.jpg

# Then re-run batch-recognize — Jane will show up with confidence >0.9
```

For unsupervised pipelines (no known names), skip this step and treat
`person_0`, `person_1`, ... as pseudonymous identities.

### Step 4 — Write to SurrealDB

Convert cluster output into `person` nodes + `appears_in` edges:

```bash
surreal_create(table="person", data= '{"id": "person_0", "label": "Person 0", "face_count": 4, "embedding": [...]}'

surreal_create(table="media", data= '{"id": "photo_108", "path": "data/.../photo_108.jpg", "type": "image"}'

python3 sandbox/the SurrealDB MCP tools relate \
    --from person:person_0 \
    --edge appears_in \
    --to media:photo_108 \
    --data '{"confidence": 0.94, "box": {...}}'
```

Or batch-insert via a small Python helper that iterates `clusters.json`.

## Smoke test

After `./bootstrap.sh`, verify the face pipeline works end-to-end with a
single known image:

```bash
set -a && . ./.env.local && set +a

# 1. Subject DB starts empty (bootstrap.sh wiped demo data)
python3 sandbox/face_client.py list-subjects
# Expected: (0 subjects) []

# 2. Add a subject (use any image with a clear face)
python3 sandbox/face_client.py add-subject --name "TestSubject" --image <face.jpg>

# 3. Recognize the same image → confidence should be 1.0
python3 sandbox/face_client.py recognize --image <face.jpg> --min-similarity 0.5

# 4. Cluster a batch — verify HDBSCAN runs cleanly
python3 sandbox/face_client.py cluster --input <batch.json> --output /tmp/c.json
# Expected: "Found N identity clusters from M faces" on stderr

# 5. Cleanup
python3 sandbox/face_client.py delete-subject --name "TestSubject"
```

## Troubleshooting

- **HTTP 400 "No face is found in the given image"** — image has no detectable
  face (screenshot, landscape, etc.). Filter these out before clustering or
  skip during batch-recognize (they appear as `faces_found: 0`).
- **HTTP 500 with "Model type /X does not exists"** — wrong URL path. All
  recognition endpoints require the `/recognition/` prefix:
  `/api/v1/recognition/recognize`, `/api/v1/recognition/faces`,
  `/api/v1/recognition/subjects`.
- **HTTP 401/403** — `COMPREFACE_API_KEY` missing or wrong. Check `.env.local`.
- **`No module named 'hdbscan'`** — falls back to scikit-learn DBSCAN
  automatically. For full HDBSCAN, `pip install hdbscan` in the sandbox image.
- **0 clusters found** — likely legitimate (all faces genuinely unique). To
  verify clustering itself works, run the synthetic test in the smoke test
  section above (4 copies of an embedding + 4 noise → 1 cluster).
