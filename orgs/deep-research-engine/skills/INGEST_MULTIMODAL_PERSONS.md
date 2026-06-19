# INGEST_MULTIMODAL_PERSONS

Cross-modal person linking. After face clustering (`INGEST_FACE_CLUSTERING_V2`) and voice diarization (`INGEST_AUDIO_DIARIZATION`) have populated `face_appearance` and `speaker_turn` records, this skill links them into unified `person` nodes.

This is the **second half** of identity resolution. The output is what makes the context engine actually useful — a `person` node with both `face_centroid` AND `voice_centroid` populated can be queried for photos, voice minutes, transcripts, and topics in one shot.

Run AFTER `INGEST_FACE_CLUSTERING_V2` and `INGEST_AUDIO_DIARIZATION` (which now uses `embed_voice` per segment).

## The lip-sync heuristic (v1)

Cross-modal linking uses **temporal co-occurrence**, not vector similarity. Face embeddings (MobileFaceNet, 512-d) and voice embeddings (WeSpeaker, 256-d) don't share a metric space — you can't cosine-compare them. The right signal is **temporal**: who's on camera when this person is speaking?

### The rule

For each `speaker_turn` (segment of voice with `start_sec` + `end_sec` in a video):

1. **Query face_appearances in the same time window:**
   ```sql
   SELECT * FROM face_appearance
   WHERE item_id = $video_id
     AND frame_sec >= $start_sec
     AND frame_sec <= $end_sec
   ```

2. **Apply the rule:**
   - **1 face** → the face's cluster and this voice's cluster are the same identity. Link them.
   - **N faces (N ≥ 2)** → ambiguous. Write a `pending_link` record. v2 will resolve.
   - **0 faces** → off-camera speaker. Write a `voice_only_person` node.

### Why "1 face = link" is acceptable for v1

In single-speaker videos (the 80% case for Telegram-style clips), there's exactly one person on camera while they talk. The heuristic assumes no cutaways. For multi-speaker videos, we defer rather than guess wrong.

## Pipeline

### Step 1 — Verify upstream

```bash
# Faces clustered?
FACE_COUNT=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM face_appearance)" \
    | jq -r '.[0].result')

# Voice turns clustered?
TURN_COUNT=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM speaker_turn)" \
    | jq -r '.[0].result')

echo "upstream: $FACE_COUNT faces, $TURN_COUNT speaker turns"
[[ "$FACE_COUNT" -eq 0 || "$TURN_COUNT" -eq 0 ]] && exit 1
```

### Step 2 — Build voice clusters (if not already)

If `speaker_turn` records have raw embeddings but no `voice_cluster_id`, cluster them now:

```bash
# Extract embeddings
curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "SELECT embedding FROM speaker_turn" \
    | jq '[.[0].result[].embedding]' > /tmp/voice_vectors.json

# Cluster via media-mcp
curl -sX POST http://localhost:8102/mcp \
    -H 'Content-Type: application/json' \
    -d @<(jq -c '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:"cluster_embeddings",arguments:{embeddings:.}}}' /tmp/voice_vectors.json) \
    > /tmp/voice_clusters.json

# Write voice_cluster_id back to each speaker_turn
# (iterate labels + speaker_turn IDs in parallel, UPDATE each)
```

### Step 3 — Walk every speaker_turn

For each video with speaker_turns:

```python
# Pseudocode — write as make_script
for video_id in all_videos_with_turns:
    turns = surreal_query(f"SELECT * FROM speaker_turn WHERE item_id = {video_id} ORDER BY start_sec")
    for turn in turns:
        # Find faces in the same time window
        faces = surreal_query(f"""
            SELECT * FROM face_appearance
            WHERE item_id = {video_id}
              AND frame_sec >= {turn.start_sec}
              AND frame_sec <= {turn.end_sec}
        """)

        unique_face_clusters = set(f.cluster_id for f in faces if f.cluster_id != -1)

        if len(unique_face_clusters) == 1:
            # LINK: face cluster == voice cluster
            face_cluster = unique_face_clusters.pop()
            voice_cluster = turn.voice_cluster_id

            # Upsert unified person node
            person_id = f"person_{face_cluster}"  # reuse face cluster ID
            surreal_upsert_person(person_id, voice_centroid=turn.embedding)

            # Edges
            surreal_relate(f"person:{person_id}", "speaks_in", f"item:{video_id}",
                          {"role": "speaker", "voice_minutes": turn.duration_sec / 60})

        elif len(unique_face_clusters) >= 2:
            # DEFER: write pending_link
            surreal_insert("pending_link", {
                "video_id": video_id,
                "start_sec": turn.start_sec,
                "end_sec": turn.end_sec,
                "face_cluster_ids": list(unique_face_clusters),
                "voice_cluster_id": turn.voice_cluster_id,
                "reason": "ambiguous_multi_face"
            })

        else:
            # 0 faces: off-camera speaker
            voice_only_id = f"voice_only_{turn.voice_cluster_id}"
            surreal_upsert_person(voice_only_id, voice_centroid=turn.embedding)
            surreal_relate(f"person:{voice_only_id}", "speaks_in", f"item:{video_id}",
                          {"role": "off_camera_speaker"})
```

### Step 4 — Cross-modal merge pass

After the per-video walk, you have:
- `person_0`, `person_1`, ... from face clustering (face_centroid only)
- `voice_only_N` from off-camera speakers (voice_centroid only)
- Linked `person_X` nodes from successful lip-sync (BOTH centroids)

For `pending_link` records where the voice_cluster_id matches a successfully linked person in another video, resolve the link:

```python
for pending in pending_links:
    # Did this voice_cluster_id get resolved elsewhere?
    resolved = surreal_query(f"""
        SELECT id, face_centroid FROM person
        WHERE voice_centroid IS NOT NONE
          AND id IN (SELECT VALUE ->speaks_in<-person FROM item
                     WHERE voice_cluster_id = {pending.voice_cluster_id})
    """)
    if resolved:
        # The voice cluster = this face cluster; resolve pending
        surreal_delete("pending_link", pending.id)
        # Update face cluster's person to add voice_centroid
```

### Step 5 — Final person rollup

After all linking, every `person` should have at least one of `face_centroid` / `voice_centroid`. Compute aggregate fields:

```bash
for person in persons:
    face_count = count(face_appearance where cluster_id == person.face_cluster_id)
    voice_minutes = sum(speaker_turn.duration_sec where voice_cluster_id == person.voice_cluster_id) / 60
    update person set face_count = $face_count, voice_minutes = $voice_minutes
```

## Smoke test

End-to-end on a tiny dataset:
```bash
# 1 video, 1 speaker, 30 seconds
# Expected: 1 person node with face_centroid + voice_centroid + appears_in + speaks_in edges

# Verify
curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "SELECT *, ->appears_in->item.path AS photos, ->speaks_in->item.id AS videos FROM person WHERE face_centroid != NONE AND voice_centroid != NONE"
```

Expected: at least 1 person with non-empty `photos` + `videos` arrays.

## Pitfalls

1. **Frame_sec vs message timestamp.** `face_appearance.frame_sec` is seconds from start of video. `speaker_turn.start_sec` is also seconds from start of video. Don't confuse either with the Telegram message timestamp (which is wall-clock).
2. **Cluster ID collision.** Face cluster IDs (`person_0`, `person_1`) and voice cluster IDs (`voice_cluster_0`, `voice_cluster_1`) come from independent HDBSCAN runs. The ID space collides if you reuse integers. Disambiguate via prefix (`person_fc_0` vs `person_vc_0`) or via separate tables until linking is complete.
3. **Don't pad voice embeddings to face dim.** 256-d voice + 512-d face — leave them in separate fields. The `person` node has `face_centroid[512]` and `voice_centroid[256]` as separate optional fields.
4. **`pending_link` is not a failure.** v1 defers multi-face cases by design. The auditor counts these as `ambiguous_deferred` and they're expected for any video with cutaways.
5. **Embeddings must be saved.** Re-running this skill on existing `face_appearance` records requires their `embedding` field to be intact. If you wiped embeddings after clustering (to save space), re-run `INGEST_FACE_CLUSTERING_V2` from step 2.

## Verification

```bash
# Cross-modal success rate
LINKED=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM person WHERE face_centroid != NONE AND voice_centroid != NONE)" \
    | jq -r '.[0].result')

# Deferred (multi-face) cases
DEFERRED=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM pending_link)" \
    | jq -r '.[0].result')

# Off-camera speakers
VOICE_ONLY=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM person WHERE face_centroid = NONE AND voice_centroid != NONE)" \
    | jq -r '.[0].result')

echo "linked: $LINKED, deferred: $DEFERRED, voice_only: $VOICE_ONLY"
```

For the Telegram dataset, expect roughly: linked ≥ 1 (criteria #6), deferred depends on video count, voice_only depends on off-camera speech.
