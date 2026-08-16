# PIPELINE_RUNBOOK

**The deterministic-to-LLM ingest pipeline.** Read this when running a
multimodal ingest task (chat dumps, scraped corpora, audio/video/photo sets).
Covers Step 0 (preprocessing), Step 0.5 (deterministic identity resolution +
preliminary dossiers), and Step 2 (LLM identity labeling). For cross-modal
linking strategies (temporal co-occurrence, voice→sender, face co-occurrence),
read INGEST_MULTIMODAL_PERSONS.md instead/after.

## Step 0 — Deterministic Pre-Processing (RUN THIS FIRST)

The raw data folder arrives via `$DATA_DIR` (set by the `--data` parameter on
direct invocation — the path is NEVER in your task/prompt). Your first action is to
run the deterministic preprocessing pipeline on it:

```bash
# Resolve the data folder from the environment — do NOT ask, do NOT guess.
echo "DATA_DIR=$DATA_DIR"
RUN_DIR="artifacts/run-$(date +%Y-%m-%d)/"
mkdir -p "$RUN_DIR"
python3 scripts/preprocess_pipeline.py \
  --data "$DATA_DIR" --run-dir "$RUN_DIR" [--workers N]
```

This produces structured JSON artifacts that you read directly.
**NEVER re-run media processing as reasoning** — faces, OCR, VLM,
classification, object detection, and scene detection are ALL done by the
pipeline above. After it completes, you do ONLY reasoning: entity resolution,
dossier building, synthesis, audit.

**Pre-processing artifacts (in `$RUN_DIR/`):**
- `items.json` — parsed chat items (also loaded to SurrealDB as `item` rows)
- `image_classification.json` — photo categories (text_screenshot,
  photo_people, document, meme, object)
- `ocr_results.json` — transcribed text from screenshots/documents
- `video_frame_analysis.json` — VLM descriptions of video keyframes
- `object_detection.json` — YOLOv8 detections (labels, confidence, bbox)
- `face_embeddings.json` + `face_clusters.json` — InsightFace 512-d
  embeddings + HDBSCAN clusters
- `voice_embeddings.json` + `voice_clusters.json` — WeSpeaker 256-d
  (when pyannote.audio is installed in media-mcp)
- `video_scenes.json` — PySceneDetect scene boundaries
- `preprocessing_manifest.json` — summary of all artifacts + counts

**Known infrastructure gaps (Dockerfile-level):**
- ASR: Parakeet ONNX model incompatible with ONNX Runtime 1.27.0.
  Workaround: use transcripts from prior agent run (`audio_chunks.json`,
  `all_audio_corpus.json`, `audio_summaries.json`).
- Voice embeddings: `pyannote.audio` not in media-mcp Docker image.
  Workaround: face clusters + text co-occurrence for identity resolution.
- kosmos_ocr: disabled. OCR uses `cloud_vlm` (MiMo-V2.5) instead.

## Step 0.5 — Deterministic Identity Resolution + Preliminary Dossiers

Immediately after Step 0, run the deterministic resolver and the first dossier
pass. These are CODE — no reasoning. They produce:

1. `person:voice_cluster_N` nodes + `speaks_in` edges (audio items linked to
   speaker clusters).
2. `person:face_cluster_N` stubs with whatever deterministic signal exists
   (sender attribution recorded as `distributed_by`, OCR mentions as
   `resolved_identity`).
3. `same_as` edges between clusters that co-occur in video items (face in
   keyframes + voice in audio of the SAME video ⇒ likely same person).
4. `entities/` folders for EVERY entity including unresolved clusters as
   `face_cluster_N` / `voice_cluster_N` pseudo-entities.

```bash
# 0. Re-cluster at IDENTITY level (agglomerative cosine, not HDBSCAN
#    min_cluster_size=2 which fragments each person into many tiny
#    near-duplicate clusters). Reads *_embeddings.json, overwrites
#    *_clusters.json. Run AFTER Step 0 produced embeddings, BEFORE ingest.
RUN_DIR="artifacts/run-$(date +%Y-%m-%d)/"
python3 plugins/deep-research/skills/deep-research/scripts/recluster.py "$RUN_DIR" 0.80 0.30   # face_thr voice_thr

# 1. Ingest artifacts into SurrealDB (items, faces, voice clusters, edges).
python3 plugins/deep-research/skills/deep-research/scripts/pipeline_ingest.py --run-dir "$RUN_DIR" --skip-embeddings

# 2. Deterministic identity resolution (creates voice_cluster nodes,
#    resolves senders, cross-links via video co-occurrence, writes
#    same_as edges). Idempotent.
python3 plugins/deep-research/skills/deep-research/scripts/resolve_identities.py "$RUN_DIR"

# 3. Preliminary dossier build — surfaces all entities + clusters.
python3 plugins/deep-research/skills/deep-research/scripts/build_entity_dossiers.py "$RUN_DIR"
```

After this step:
- Named entities (e.g. `Christopher_Anthony_Semok/`) have populated `text/`
  (text mentions — proven) plus `photos/` and `audio/` **only when** the
  deterministic resolver already linked a cluster to them.
- Clusters appear under `clusters/` as `face_cluster_N/` and
  `voice_cluster_N/` with their media under `photos/` and `audio/`.
- Named entities' `photos/` and `audio/` folders are EMPTY until a cluster
  is linked to them via a `same_as` edge — either by the deterministic
  resolver or by the LLM identity resolution step below.

## Step 2 — LLM Identity Resolution (reasoning)

The deterministic resolver handles the easy cases (sender attribution, OCR
text matches, video co-occurrence). But many face clusters remain as
`face_cluster_N` pseudo-entities because their photos don't contain OCR
labeling who's in them. An investigator (you, the LLM) reads the surrounding
context and proposes labels.

**Procedure:**
1. Read `entities/index.md` to see the full entity list.
2. For each `face_cluster_N/` folder:
   - Open `face_cluster_N/face_cluster_N.md` and read the OCR excerpts,
     sender list, and surrounding message text.
   - Look at the photo paths listed. If VLM descriptions are available in
     `$RUN_DIR/video_frame_analysis.json` or image classification in
     `image_classification.json`, reason about what the cluster depicts.
   - Cross-reference against named entities: does this cluster appear in
     photos sent BY a named entity? Does it appear in photos near text
     mentioning a specific person? Does video co-occurrence link it to a
     voice cluster that resolved to a sender?
   - If you can propose a label with confidence ≥ 0.6, write a `same_as`
     edge linking the cluster to the named entity using the helper CLI:
     ```bash
     python3 plugins/deep-research/skills/deep-research/scripts/link_cluster.py face_cluster_2 ent_Christopher_Semok \
         --confidence 0.75 \
         --signal llm_visual_context_reasoning \
         --reasoning "Cluster photos show screenshots of Telegram convos \
                      about Commissar (Semok alias). Sender = Semok."
     ```
     Alternatively, use `surreal_query(sql="...")` with the RELATE
     statement directly — but prefer the helper (idempotency + correct
     SurrealDB record ID escaping).
   - If you CANNOT propose a label with confidence, leave the cluster as
     `face_cluster_N`. Better an unlabeled cluster than a wrong attribution.
3. Do the same for `voice_cluster_N/` folders where deterministic sender
   attribution may be wrong (sender ≠ speaker). Read transcript excerpts: if
   the speaker refers to themselves in third person as the sender, the
   attribution is likely correct; if other voices address them by a different
   name, correct it.
4. After writing all proposed `same_as` edges, rebuild the dossiers so named
   entities' `photos/` and `audio/` populate from the newly-linked clusters:
   ```bash
   python3 plugins/deep-research/skills/deep-research/scripts/build_entity_dossiers.py "$RUN_DIR"
   ```

### Discipline — clustering is the SOLE determiner of media in an entity folder

- **Text mentions** — proven, always include (already done by code). Surface
  as `text/mentions.md` (plaintext) and `text/audio_mentions.md` (transcript
  excerpts). Text tells you the entity EXISTS; it does NOT tell you what the
  entity LOOKS/SOUNDS like.
- **Face cluster linkage (`photos/`)** — the ONLY way a photo lands in an
  entity folder. A `same_as` edge links `person:face_cluster_N` to
  `person:ent_<Name>`, and the dossier builder symlinks every photo in that
  cluster into `<Name>/photos/`. This is "photos OF the entity".
- **Voice cluster linkage (`audio/`)** — the ONLY way an audio file lands in
  an entity folder. `person:voice_cluster_N -> same_as -> person:ent_<Name>`
  populates `<Name>/audio/`. This is "audio OF the entity".
- **NEVER use sender attribution.** "Who posted this" ≠ "who this is a photo
  of." Sender attribution recreates the exact random-photos problem this
  pipeline was built to kill. Sender value is captured in text excerpts as
  context, nothing more.
- **NEVER use stream-index proximity.** Photos "near" a text mention in a
  channel are almost never photos OF the mentioned person.
