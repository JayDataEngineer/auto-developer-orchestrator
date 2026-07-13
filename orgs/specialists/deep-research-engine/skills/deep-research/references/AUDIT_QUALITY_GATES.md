# AUDIT_QUALITY_GATES

The 7 measurable success criteria for deep-research-engine ingestion. Run all 7 in order. Each returns pass/fail with concrete numbers + sample bad rows. The auditor role uses this skill as its checklist.

The CTO loop: `delegate_to ingestion-director → delegate_to auditor → if gaps, delegate_to ingestion-director with refined scope → repeat` until all 7 pass.

## Scope

**Checks 1-6 apply specifically to the multimodal-ingest path** — any task that populates the `item`/`transcript`/`media`/`person` tables via the format-specific parsers + `audio_client.py` + `video_frames.py` + the face-clustering skills.

**Check 7 (embedding_coverage_complete) is general — it applies to ANY path that populates a vector column.** When auditing a web-research-only or PDF-only task, skip checks 1-6 and run only check 7 against whatever tables the task actually populated. The check is table-driven: a query that references an empty table returns 0 missing, which is a trivial pass — not a false positive.

If your task created tables not covered by check 7's hardcoded list (e.g. `pdf_chunk` from a future PDF pipeline), add a count query for that table's embedding column before yielding. Don't ship a new vector column without a coverage check.

## Setup

```bash
# Required env (loaded by sandbox workers)
export SURREAL_PASSWORD=root    # matches docker-compose.yml surrealdb service
```

All queries go directly to SurrealDB on `http://localhost:8000/sql` with headers `surreal-ns: research` + `surreal-db: main` (SurrealDB v3.1+ requires lowercase header names — the uppercase `NS`/`DB` form was deprecated).

## Check 1 — transcripts_complete

**Goal:** every `item` of type `voice` or `video` has a `transcript` child with non-empty `text`. Target: 100% coverage of populated voice/video items.

```bash
# Count voice/video items missing a transcript OR with empty-text transcript.
# SurrealDB v3 syntax: array::len() on graph-traversed field.
# ->transcribed_by->transcript.text returns array of text values from linked transcripts.
MISSING=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d 'SELECT count() FROM item WHERE type IN ["voice", "video"] AND (count(->transcribed_by->transcript) = 0 OR array::len(->transcribed_by->transcript.text) = 0) GROUP ALL' \
    | jq -r '.[0].result[0].count')

TOTAL=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d 'SELECT count() FROM item WHERE type IN ["voice", "video"] GROUP ALL' \
    | jq -r '.[0].result[0].count')

echo "transcripts: $((TOTAL - MISSING))/$TOTAL"
```

**Pass:** MISSING = 0.
**Fail sample:** list first 5 missing IDs:
```bash
curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d 'SELECT id FROM item WHERE type IN ["voice", "video"] AND (count(->transcribed_by->transcript) = 0 OR array::len(->transcribed_by->transcript.text) = 0) LIMIT 5' \
    | jq -r '.[0].result[].id'
```

**Common failure cause:** ASR provider 429'd mid-batch. Re-delegate INGEST_AUDIO_DIARIZATION with the explicit list of missing IDs.

**Parametric thresholds:** "34/34" appears in the goal above as an example baseline from one stress-test corpus. The real threshold is **100% coverage of populated rows**. Compute total voice/video items at audit time and require `MISSING = 0`.

**Silent videos — legitimate edge case:** some videos are recorded with no microphone input (audio measures -91 dB = digital silence). ASR correctly returns empty text. The pipeline writes a transcript with `text="[no speech detected ...]"` and `is_silent: true` so the audit reflects reality rather than masking silence as a failure. The query above counts these as success because `text` is non-empty. If you want to see how many transcripts are silent markers vs real speech:

```bash
curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN { speech: count(SELECT id FROM transcript WHERE is_silent != true), silent: count(SELECT id FROM transcript WHERE is_silent = true) }" \
    | jq -r '.[0].result'
```

## Check 2 — sender_names_clean

**Goal:** zero `sender` values contain a timestamp. Target: 0.

```bash
# SurrealDB v3 regex: use string::matches() with double-escaped backslashes.
# Single quote outside, double backslash inside the SQL literal.
POLLUTED=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM item WHERE string::matches(sender, '\\\\d{2}\\\\.\\\\d{2}\\\\.\\\\d{4}'))" \
    | jq -r '.[0].result')
```

**Pass:** POLLUTED = 0.
**Common failure cause:** parser regex broken, or someone re-ingested via raw scraping instead of the canonical parser for the source format.

## Check 3 — sender_extraction_worked

**Goal:** <5% of items have `sender='Unknown'`. Target: rate < 5% of total items.

```bash
UNKNOWN=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM item WHERE sender = 'Unknown')" \
    | jq -r '.[0].result')

TOTAL=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM item)" \
    | jq -r '.[0].result')

echo "unknown rate: $(echo "scale=2; $UNKNOWN * 100 / $TOTAL" | bc)%"
```

**Pass:** rate < 5%.
**Disambiguation:** Genuine `Unknown` (forwarded messages, deleted accounts, orphan media) is fine. Sample 10 Unknown senders and check the raw export — if they're all forwarded messages or orphan media files, the parser is working correctly. If they're normal messages with a visible sender in the source, the parser missed them.

## Check 4 — topic_discovery_ran

**Goal:** `topic` table has ≥5 rows. Target: ≥5.

```bash
TOPICS=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM topic)" \
    | jq -r '.[0].result')
```

**Pass:** TOPICS ≥ 5.
**Common failure cause:** LLM 429'd or returned malformed JSON during entity extraction. Re-delegate INGEST_ENTITY_EXTRACTION.

## Check 5 — person_clusters_exist

**Goal:** ≥3 distinct `person` clusters from face+voice clustering. Target: ≥3.

```bash
PERSONS=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM person)" \
    | jq -r '.[0].result')
```

**Pass:** PERSONS ≥ 3.
**Common failure cause:** face clustering didn't run (check `face_appearance` count), OR clustering parameters too aggressive (lower `min_cluster_size`).

## Check 6 — cross_modal_linking_worked

**Goal:** ≥1 `person` node has BOTH `face_centroid` AND `voice_centroid`. Target: ≥1.

```bash
LINKED=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM person WHERE face_centroid != NONE AND voice_centroid != NONE)" \
    | jq -r '.[0].result')
```

**Pass:** LINKED ≥ 1.
**Note:** This check only passes if checks #5 ran first. If #5 fails, #6 trivially fails too — note this in the report and move on.

## Check 7 — embedding_coverage_complete

**Goal:** every vector column is populated on every row that should have one. A 4%-populated HNSW index is a trap — semantic search silently misses 96% of the corpus. Target: 0 missing on each count below.

```bash
# Items without text embeddings
ITEMS_NO_VEC=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM item WHERE text_embedding = NONE OR array::len(text_embedding) != 1024)" \
    | jq -r '.[0].result')

# Transcripts without embeddings
TR_NO_VEC=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM transcript WHERE embedding = NONE)" \
    | jq -r '.[0].result')

# Face appearances without embeddings (orphan detection vectors)
FACE_NO_VEC=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM face_appearance WHERE embedding = NONE)" \
    | jq -r '.[0].result')

# Topics without centroid embeddings (can't be semantic-search targets)
TOPIC_NO_VEC=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM topic WHERE centroid_embedding = NONE)" \
    | jq -r '.[0].result')

# Orphan media (videos registered but never processed through video_frames.py)
MEDIA_NO_TYPE=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM media WHERE type = NONE)" \
    | jq -r '.[0].result')

# Orphan persons (no face or voice evidence linked)
PERSON_NO_EDGE=$(curl -sX POST http://localhost:8000/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM person WHERE count(<-appears_in<-face_appearance) = 0 AND count(<-speaks_in<-speaker_turn) = 0)" \
    | jq -r '.[0].result')

echo "embedding coverage gaps:"
echo "  item.text_embedding:        $ITEMS_NO_VEC"
echo "  transcript.embedding:       $TR_NO_VEC"
echo "  face_appearance.embedding:  $FACE_NO_VEC"
echo "  topic.centroid_embedding:   $TOPIC_NO_VEC"
echo "  media.type (orphans):       $MEDIA_NO_TYPE"
echo "  person with no edges:       $PERSON_NO_EDGE"
```

**Pass:** ALL counts = 0.

**Common failure causes + fixes:**
- `item.text_embedding > 0`: ingestion-director ran embedding on a subset (the 13 it found interesting, not the other 351). Re-delegate INGEST_ENTITY_EXTRACTION with explicit instruction: "vectorize every item, not just entities — backfill the rest."
- `transcript.embedding > 0`: ASR happened but the embedding step was skipped for placeholder transcripts. Either embed the placeholder text too or mark `transcript_status='failed'` so future queries can filter.
- `face_appearance.embedding > 0`: shouldn't happen — embed_faces always returns vectors or fails entirely. If non-zero, media-mcp was returning malformed responses. Re-delegate.
- `topic.centroid_embedding > 0`: cluster created but centroid never computed. Re-delegate INGEST_ENTITY_EXTRACTION with the specific topic IDs.
- `media.type = NONE > 0`: items registered as media rows without going through the proper pipeline. For orphans with `path=<video_subdir>/*`, run `video_frames.py process` on each. For others, identify the source and re-ingest.
- `person with no edges > 0`: person node created speculatively but no face/voice evidence was linked. Either link the evidence or delete the orphan person.

**Principle:** see MANIFESTO principle #7 (Comprehensive persistence). The agent's job isn't done when *some* data is in the DB — it's done when the DB is usable as a reference for future queries.

## When all 6 pass

Report `overall: pass`. The CTO can now safely delegate to the artifact-generation phase (research-director + artifact-director) knowing the underlying knowledge graph is trustworthy.

If any check still fails after 5 iteration rounds, escalate to the user with the concrete failure mode + proposed fix. Don't keep retrying the same thing hoping it works.
