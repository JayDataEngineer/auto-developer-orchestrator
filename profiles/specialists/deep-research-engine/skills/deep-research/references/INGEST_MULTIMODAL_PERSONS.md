# INGEST_MULTIMODAL_PERSONS

**Dynamic cross-modal identity linking.** This is the skill that turns separate
face clusters + voice clusters + extracted names into unified `person` identities.

**Read this AFTER face clustering, voice clustering, and entity extraction have
run.** You are linking signals they produced — you do NOT cluster or extract here.

## The core principle: DYNAMIC, not hardcoded

Every dataset is different. Some have photos + voice notes. Some have only
video. Some have text only. Some have forwarded photos (sender ≠ subject).
**You cannot hardcode one linking pipeline.** Instead:

1. **INSPECT** what signals exist in THIS dataset
2. **CHOOSE** which linking strategies apply (table below)
3. **EXECUTE** them via SurrealQL
4. **REASON** about ambiguous cases with LLM judgment

Never assume a modality is present. Always query first.

## Step 1 — Inspect what signals exist

Run these counts. The answers determine which strategies are available:

```
surreal_query(sql="
  RETURN count(SELECT id FROM face_appearance);              -- faces detected?
  RETURN count(SELECT id FROM face_appearance WHERE cluster_id != -1);  -- face clusters?
  RETURN count(SELECT id FROM person WHERE voice_cluster_id != NONE);   -- voice clusters?
  RETURN count(SELECT id FROM speaker_turn);                 -- diarized turns (with timestamps)?
  RETURN count(SELECT id FROM item WHERE type = 'video');    -- videos present?
  RETURN count(SELECT id FROM item WHERE type = 'voice');    -- audio-only items?
  RETURN count(SELECT id FROM item WHERE sender != NONE AND sender != 'Unknown');  -- sender attribution?
  RETURN count(SELECT id FROM person WHERE role = 'mentioned');  -- entity-extracted names?
  RETURN count(SELECT id FROM appears_in);                   -- face→item links?
  RETURN count(SELECT id FROM speaks_in);                    -- voice→item links?
")
```

**Decision table** — match signals to strategies:

| Signal present | Strategy | Strength | Section |
|---|---|---|---|
| Videos with both face + voice turns (timestamped) | **A. Temporal co-occurrence** (lip-sync) | Very strong | Step 2A |
| Videos with face_appearance + speaks_in edges | **B. Video item co-occurrence** | Strong | Step 2B |
| Voice items with sender attribution | **C. Voice → sender** | Strong | Step 2C |
| Transcripts/captions mention names | **D. Name → cluster** (LLM) | Medium | Step 2D |
| Photos with multiple faces | **E. Face co-occurrence graph** | Relationship (not identity) | Step 2E |
| Photos with sender | **F. Face → sender** | WEAK — hint only | Step 2F |

**You choose which to run.** If a dataset has no video, skip A and B entirely.
If no sender metadata, skip C and F. If no transcript text, skip D.

## Step 2 — Execute the applicable strategies

### 2A. Temporal co-occurrence (lip-sync) — strongest

**Only if:** `speaker_turn` table is non-empty AND face_appearance has `frame_sec`
timestamps AND videos exist.

For each `speaker_turn` (a voice segment with start/end seconds in a video):

```
# Find faces on screen during this voice segment
surreal_query(sql="
  SELECT cluster_id, item_id FROM face_appearance
  WHERE item_id = $video_id
    AND frame_sec >= $start_sec
    AND frame_sec <= $end_sec
    AND cluster_id != -1
  GROUP BY cluster_id
")
```

Decision:
- **1 face cluster** → this voice cluster and this face cluster are the same identity. RELATE `person:voice_cluster_X -> same_as -> person:face_cluster_Y`.
- **0 faces** → off-camera speaker. Create `person:voice_only_X`.
- **2+ faces** → ambiguous. Write a `pending_link` record, defer.

### 2B. Video item co-occurrence — strong (no timestamps needed)

**Only if:** videos exist with both `appears_in` (face) and `speaks_in` (voice) edges.

```
# Videos where a face cluster and voice cluster both appear
surreal_query(sql="
  SELECT id,
    ->appears_in->person.face_cluster_id AS face_clusters,
    <-speaks_in<-person.voice_cluster_id AS voice_clusters
  FROM item WHERE type = 'video'
")
```

For each video: every (face_cluster, voice_cluster) pair that co-occur are
candidate same-identities. Create `same_as` edges with `evidence: 'video co-occurrence'`.
If a face+voice pair co-occur in 2+ videos, confidence is high.

### 2C. Voice → sender attribution — strong

**Only if:** voice/audio items have a `sender` field. This is generic — works
for any messaging dataset (Telegram, WhatsApp, Slack, email).

You send your own voice. So the sender of a voice item IS the speaker:

```
# Which sender does each voice cluster belong to?
surreal_query(sql="
  SELECT voice_cluster_id,
         <-speaks_in<-item.sender AS senders,
         count() AS n
  FROM person
  WHERE voice_cluster_id != NONE
  SPLIT senders
  GROUP BY voice_cluster_id, senders
")
```

If a voice cluster's items are predominantly (≥80%) from one sender, resolve:
`UPSERT person:resolved_<sender> ...` + `RELATE voice_cluster -> same_as -> resolved`.

**Caveat:** forwarded voice messages break this (the forwarder isn't the
speaker). Check the `forwarded` field on items — if true, skip.

### 2D. Name → cluster via LLM reasoning — medium

**Only if:** transcripts or captions contain names (entity extraction produced
`person:ent_*` nodes).

This is where YOU (the LLM) add value a script can't. Reason about context:

1. Query which names appear in which transcripts:
   ```
   surreal_query(sql="
     SELECT <-extracted_from<-source.text AS context
     FROM person:ent_grady
   ")
   ```
2. Query the transcripts where each voice cluster speaks:
   ```
   surreal_query(sql="
     SELECT ->speaks_in->item.text AS said_by_cluster
     FROM person:voice_cluster_5
   ")
   ```
3. **Reason:** if transcript text says "Grady" while cluster 5 is speaking,
   cluster 5 is probably Grady. Create the `same_as` edge.

This requires judgment — do it yourself, don't try to script it.

### 2E. Face co-occurrence graph — relationship, not identity

**Only if:** photos contain 2+ detected faces.

Faces appearing together across multiple photos are RELATED (friends,
associates) — not the same identity. Build a social graph:

```
# For each photo, which face clusters appear together?
surreal_query(sql="
  SELECT item_id, cluster_id FROM face_appearance
  WHERE cluster_id != -1 ORDER BY item_id
")
```

For each photo with faces [A, B, C]: create `associated_with` edges between
every pair. This is a relationship graph, not an identity claim.

### 2F. Face → sender — WEAK, hint only

**Only if:** photos have sender attribution.

The sender of a photo MIGHT be the subject, but forwarding breaks this
(people forward others' photos). Record the distributor, do NOT claim identity:

```
UPDATE person:face_cluster_X SET distributed_by = $sender,
  notes = 'Photos of this face were distributed by ' + $sender +
          '. Sender is the distributor, NOT necessarily the subject.'
```

Never create a `same_as` edge from this signal alone.

## Step 3 — Create unified identity nodes

After linking, group clusters that are `same_as`-connected into canonical
identity nodes:

```
UPSERT person:resolved_<name> SET
  canonical_name = $name,
  role = 'resolved_identity',
  face_cluster_ids = $linked_face_clusters,
  voice_cluster_ids = $linked_voice_clusters;
```

Then everything connects: a photo → `appears_in` → face_cluster → `same_as` →
resolved identity ← `same_as` ← voice_cluster ← `speaks_in` ← audio clip.
One query surfaces all media for a person across all modalities.

## Step 4 — Audit

```
surreal_query(sql="
  RETURN count(SELECT id FROM same_as);
  RETURN count(SELECT id FROM person WHERE role = 'resolved_identity');
  RETURN count(SELECT id FROM pending_link);
")
```

If `pending_link` is non-empty, there are AMBIGUOUS cases (2+ faces during one
voice segment). Review them manually and resolve or leave deferred.

## Common pitfalls

- **Don't assume video exists.** Most messaging datasets are photos + voice
  notes, no video. Skip strategies A and B if `count(video items) = 0`.
- **Don't assume sender = subject for photos.** Forwarded photos, news
  clippings, and screenshots break face→sender attribution.
- **Don't link across metric spaces.** Face embeddings (512-d MobileFaceNet)
  and voice embeddings (256-d WeSpeaker) can't be cosine-compared. Use
  co-occurrence, not vector similarity, to bridge modalities.
- **Empty clusters are noise.** `cluster_id = -1` is HDBSCAN noise — never
  link or resolve it.

## Implementation

The strategies above are implemented in `sandbox/resolve_identities.py`. Run it
after `close_graph_gaps.py`:

```bash
python3 sandbox/resolve_identities.py
```

It executes all 6 steps automatically: face distributor recording, voice
cluster node creation + sender resolution, video audio → voice cluster linking,
video keyframe → face cluster linking (heuristic), voice → identity same_as
edges, and face ↔ voice cross-linking via video co-occurrence.

### Prerequisites

This script assumes these have already run:
1. `face_client.py` — face detection + embeddings
2. `voice_embed.py` — voice clustering (resemblyzer)
3. `close_graph_gaps.py` — entity extraction, person nodes, appears_in edges
4. `video_summarize.py` — (optional) video summaries + diarization.
   See `SUMMARIZE_VIDEOS.md`. Not required for identity linking, but video
   summaries enrich the graph with `video_summary` fields on item records.

### Video audio in voice clusters

Voice clustering (`voice_embed.py`) processes ALL audio files, including
video audio tracks extracted as `IMG_XXXX.wav`. This means video items
automatically get voice cluster assignments. `resolve_identities.py` creates
`speaks_in` edges from voice cluster person nodes to video items based on
these assignments — no separate video audio processing needed.
- **One good signal beats three weak ones.** A single video co-occurrence
  (strategy B) is stronger evidence than sender attribution alone (C+F).
