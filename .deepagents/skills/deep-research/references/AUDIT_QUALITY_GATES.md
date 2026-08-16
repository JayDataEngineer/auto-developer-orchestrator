# AUDIT_QUALITY_GATES

The 7 measurable success criteria for deep-research-engine ingestion. Run all 7 in order. Each returns pass/fail with concrete numbers + sample bad rows. The auditor role uses this skill as its checklist.

The CTO loop: `delegate_to ingestion-director → delegate_to auditor → if gaps, delegate_to ingestion-director with refined scope → repeat` until all 7 pass.

## Scope

**Checks 1-6 apply specifically to the multimodal-ingest path** — any task that populates the `item`/`transcript`/`media`/`person` tables via the format-specific parsers + `audio_client.py` + `video_frames.py` + the face-clustering skills.

**Check 7 (embedding_coverage_complete) is general — it applies to ANY path that populates a vector column.** When auditing a web-research-only or PDF-only task, skip checks 1-6 and run only check 7 against whatever tables the task actually populated. The check is table-driven: a query that references an empty table returns 0 missing, which is a trivial pass — not a false positive.

If your task created tables not covered by check 7's hardcoded list (e.g. `pdf_chunk` from a future PDF pipeline), add a count query for that table's embedding column before yielding. Don't ship a new vector column without a coverage check.

## How to run queries

EVERY query in this skill is run via the `surreal_query` tool. You call it by name — you never construct URLs, run curl, or manage auth. The tool handles the SurrealDB connection internally.

```
surreal_query(sql="<SurrealQL statement>")
```

Returns JSON results. For quick table counts, use `surreal_query(sql="RETURN count(SELECT id FROM item)")`.

## Check 1 — transcripts_complete

**Goal:** every `item` of type `voice` or `video` has a `transcript` child with non-empty `text`. Target: 100% coverage of populated voice/video items.

```
# Count voice/video items missing a transcript OR with empty-text transcript.
surreal_query(sql="SELECT count() FROM item WHERE type IN ['voice', 'video'] AND (count(->transcribed_by->transcript) = 0 OR array::len(->transcribed_by->transcript.text) = 0) GROUP ALL")

# Total voice/video items
surreal_query(sql="SELECT count() FROM item WHERE type IN ['voice', 'video'] GROUP ALL")
```

**Pass:** MISSING = 0.
**Fail sample:** list first 5 missing IDs:
```
surreal_query(sql="SELECT id FROM item WHERE type IN ['voice', 'video'] AND (count(->transcribed_by->transcript) = 0 OR array::len(->transcribed_by->transcript.text) = 0) LIMIT 5")
```

**Common failure cause:** ASR provider 429'd mid-batch. Re-delegate INGEST_AUDIO_DIARIZATION with the explicit list of missing IDs.

**Parametric thresholds:** "34/34" appears in the goal above as an example baseline from one stress-test corpus. The real threshold is **100% coverage of populated rows**. Compute total voice/video items at audit time and require `MISSING = 0`.

**Silent videos — legitimate edge case:** some videos are recorded with no microphone input (audio measures -91 dB = digital silence). ASR correctly returns empty text. The pipeline writes a transcript with `text="[no speech detected ...]"` and `is_silent: true` so the audit reflects reality rather than masking silence as a failure. The query above counts these as success because `text` is non-empty. If you want to see how many transcripts are silent markers vs real speech:

```
surreal_query(sql="RETURN { speech: count(SELECT id FROM transcript WHERE is_silent != true), silent: count(SELECT id FROM transcript WHERE is_silent = true) }")
```

## Check 2 — sender_names_clean

**Goal:** zero `sender` values contain a timestamp. Target: 0.

```
# SurrealDB v3 regex: string::matches() with double-escaped backslashes.
surreal_query(sql="RETURN count(SELECT id FROM item WHERE string::matches(sender, '\\\\d{2}\\\\.\\\\d{2}\\\\.\\\\d{4}'))")
```

**Pass:** POLLUTED = 0.
**Common failure cause:** parser regex broken, or someone re-ingested via raw scraping instead of the canonical parser for the source format.

## Check 3 — sender_extraction_worked

**Goal:** <5% of items have `sender='Unknown'`. Target: rate < 5% of total items.

```
UNKNOWN = surreal_query(sql="RETURN count(SELECT id FROM item WHERE sender = 'Unknown')")
TOTAL   = surreal_query(sql="RETURN count(SELECT id FROM item)")

# rate = UNKNOWN * 100 / TOTAL
```

**Pass:** rate < 5%.
**Disambiguation:** Genuine `Unknown` (forwarded messages, deleted accounts, orphan media) is fine. Sample 10 Unknown senders and check the raw export — if they're all forwarded messages or orphan media files, the parser is working correctly. If they're normal messages with a visible sender in the source, the parser missed them.

## Check 4 — topic_discovery_ran

**Goal:** `topic` table has ≥5 rows. Target: ≥5.

```
surreal_query(sql="RETURN count(SELECT id FROM topic)")
```

**Pass:** TOPICS ≥ 5.
**Common failure cause:** LLM 429'd or returned malformed JSON during entity extraction. Re-delegate INGEST_ENTITY_EXTRACTION.

## Check 5 — person_clusters_exist

**Goal:** ≥3 distinct `person` clusters from face+voice clustering. Target: ≥3.

```
surreal_query(sql="RETURN count(SELECT id FROM person)")
```

**Pass:** PERSONS ≥ 3.
**Common failure cause:** face clustering didn't run (check `face_appearance` count), OR clustering parameters too aggressive (lower `min_cluster_size`).

## Check 6 — cross_modal_linking_worked

**Goal:** ≥1 `person` node has BOTH `face_centroid` AND `voice_centroid`. Target: ≥1.

```
surreal_query(sql="RETURN count(SELECT id FROM person WHERE face_centroid != NONE AND voice_centroid != NONE)")
```

**Pass:** LINKED ≥ 1.
**Note:** This check only passes if checks #5 ran first. If #5 fails, #6 trivially fails too — note this in the report and move on.

## Check 7 — embedding_coverage_complete

**Goal:** every vector column is populated on every row that should have one. A 4%-populated HNSW index is a trap — semantic search silently misses 96% of the corpus. Target: 0 missing on each count below.

```
# Items without text embeddings
surreal_query(sql="RETURN count(SELECT id FROM item WHERE text_embedding = NONE OR array::len(text_embedding) != 1024)")

# Transcripts without embeddings
surreal_query(sql="RETURN count(SELECT id FROM transcript WHERE embedding = NONE)")

# Face appearances without embeddings (orphan detection vectors)
surreal_query(sql="RETURN count(SELECT id FROM face_appearance WHERE embedding = NONE)")

# Topics without centroid embeddings (can't be semantic-search targets)
surreal_query(sql="RETURN count(SELECT id FROM topic WHERE centroid_embedding = NONE)")

# Orphan media (videos registered but never processed through video_frames.py)
surreal_query(sql="RETURN count(SELECT id FROM media WHERE type = NONE)")

# Orphan persons (no face or voice evidence linked)
surreal_query(sql="RETURN count(SELECT id FROM person WHERE count(<-appears_in<-face_appearance) = 0 AND count(<-speaks_in<-speaker_turn) = 0)")
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

## Check 8 — grounding_verification

**Goal:** the brief/report contains NO ungrounded named entities. The auditor
runs this INDEPENDENTLY — never trusts the synthesizer's own grounding check.

```
python3 sandbox/grounding_check.py check \
  --report artifacts/brief.md \
  --corpus data/<source-dir>,artifacts/<run>
```

Exit 0 = PASS, exit 1 = ungrounded entities found. If FAIL, list every
UNFOUNDED entity in the audit report and recommend re-dispatching the
synthesizer.

**Pass:** exit code 0, 0 ungrounded entities.
**Fail:** exit code 1, any ungrounded entities found.

## Coverage checks (9–14) — source-vs-processed ratio gates

These checks compare SOURCE file counts on disk against PROCESSED entries in
artifacts/DB. A pipeline stage that "succeeds" but processes only 15% of
source data is a FAILED run. These are the EXHAUSTIVE list of coverage checks
— do not invent additional checks (e.g. classification coverage, OCR coverage,
object detection coverage are NOT checks). Only the 6 checks below exist.

### Check 9 — photo_face_analysis_coverage

**Goal:** every source photo was face-analyzed. Target: ≥95% coverage.

```
# Source count: non-thumb photos on disk
find data/ -type f \( -name '*.jpg' -o -name '*.jpeg' -o -name '*.png' \) ! -name '*_thumb*' | wc -l
# Processed count
python3 -c "import json; print(len(json.load(open('artifacts/<run>/face_analysis.json'))))"
```

**Pass:** processed/source ≥ 0.95.

**IMPORTANT — correct denominator:** face_analysis.json contains ALL photos
that were processed through the face pipeline, INCLUDING those where no face
was detected. A photo with no face is still "analyzed" — it appears in
face_analysis.json with an empty faces array. Therefore the denominator is
non-thumb source photos and the numerator is ALL entries in face_analysis.json
(not just those with faces). If face_analysis.json has 219 entries and there
are 219 non-thumb source photos, coverage is 100% — even though only 36 of
those photos had detectable faces.

### Check 10 — text_message_ingestion_coverage

**Goal:** every text message in the source export was ingested as an `item` row.

```
# Source count: text messages from Telegram export result.json
python3 -c "import json; d=json.load(open('data/<export>/result.json')); print(sum(1 for m in d['messages'] if isinstance(m,dict) and m.get('text') and str(m.get('text','')).strip()))"
# Processed count
surreal_query(sql="SELECT count() FROM item WHERE type = 'message' GROUP ALL")
```

**Pass:** processed/source ≥ 0.95.

### Check 11 — video_keyframe_analysis_coverage

**Goal:** every source video has keyframe analysis.

```
# Source video count
find data/ -type f \( -name '*.mp4' -o -name '*.MP4' -o -name '*.mov' -o -name '*.m4v' \) | wc -l
# Analyzed videos
python3 -c "import json; d=json.load(open('artifacts/<run>/video_frame_analysis.json')); print(len(set(e.get('video') for e in d)))"
```

**Pass:** analyzed/source ≥ 0.95.

### Check 12 — video_summary_coverage

**Goal:** every source video has a VLM summary.

```
# Source video count (same as check 11)
find data/ -type f \( -name '*.mp4' -o -name '*.MP4' -o -name '*.mov' -o -name '*.m4v' \) | wc -l
# Summarized count
python3 -c "import json; print(len(json.load(open('artifacts/<run>/video_summaries.json'))))"
```

**Pass:** summarized/source ≥ 0.95.

### Check 13 — ghost_url_detection

**Goal:** no artifact JSON contains dead ephemeral URLs.

```
grep -rl 'http://172\.\|http://localhost' artifacts/<run>/*.json
```

**Pass:** 0 matches.
**Fail:** ANY match.

### Check 14 — entity_folder_structure

**Goal:** `entities/` is subject-based, not a modality dump.

```
# Should show entity-named folders (hundreds), NOT pipeline-stage folders
ls artifacts/<run>/entities/ | grep -vE '^(raw|other)$' | head
# Should show NO top-level modality folders
ls artifacts/<run>/entities/ | grep -E '^(face_clusters|voice_clusters|text_and_scenes|video_frames)$'
# index.md must exist
ls artifacts/<run>/entities/index.md
```

**Pass:** subject-named folders exist, index.md exists, no top-level modality folders.
**Fail:** only modality folders, no index.md, or entity folders are pipeline-stage names.

## When all checks pass

Report `overall: pass`. The CTO can now safely delegate to the artifact-generation phase (research-director + artifact-director) knowing the underlying knowledge graph is trustworthy.

If any check still fails after 5 iteration rounds, escalate to the user with the concrete failure mode + proposed fix. Don't keep retrying the same thing hoping it works.

## Data integrity checks (15–17) — internal consistency gates

These checks are NOT about whether the pipeline processed the data — they're
about whether the DB is internally consistent. A pipeline that populates the
right tables but leaves stale edges or unexplained count deltas is not clean.

### Check 15 — item_count_reconciliation

**Goal:** the DB `item` count matches the source `items.json` count, and any
difference is explained in the audit report.

```
# Source
python3 -c "import json; d=json.load(open('artifacts/<run>/items.json')); print(len(d.get('items',d) if isinstance(d,dict) else d))"
# DB
surreal_query(sql="RETURN count(SELECT id FROM item)")
```

**Pass:** delta is zero OR the delta is explicitly explained in the report
(e.g. "85 thumb photos filtered by pipeline_ingest.py — thumbs are duplicates
of original photos, not separate content").
**Fail:** delta exists with no explanation.

### Check 16 — graph_edge_hygiene

**Goal:** no duplicate or stale graph edges. Every edge points to real records.

```
# Duplicate transcribed_by edges (one item → same transcript twice)
surreal_query(sql="SELECT count() FROM item WHERE count(->transcribed_by) > 1 GROUP ALL")

# Stale edges (in/out point to non-existent records)
surreal_query(sql="RETURN count(SELECT id FROM transcribed_by WHERE in NOT IN (SELECT id FROM item))")
```

**Pass:** 0 duplicates, 0 stale edges. Edge count for transcribed_by should
match the voice/video item count (one edge per item is expected cardinality).
**Fail:** any duplicates or stale edges found.

### Check 17 — cross_modal_label_arithmetic

**Goal:** when the auditor reports face-only and voice-only counts in check #6,
the overlap (persons with BOTH centroids) is subtracted. This is an arithmetic
consistency check, not a new query.

If face_total = 17, voice_total = 12, both = 1, then the report MUST say:
- face-only = 16 (17 − 1)
- voice-only = 11 (12 − 1)

**Pass:** reported "only" counts correctly subtract the intersection.
**Fail:** "only" counts include the overlap (inflated numbers).
