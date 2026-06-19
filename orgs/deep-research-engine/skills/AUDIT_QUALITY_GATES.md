# AUDIT_QUALITY_GATES

The 6 measurable success criteria for deep-research-engine ingestion. Run all 6 in order. Each returns pass/fail with concrete numbers + sample bad rows. The auditor role uses this skill as its checklist.

The CTO loop: `delegate_to ingestion-director → delegate_to auditor → if gaps, delegate_to ingestion-director with refined scope → repeat` until all 6 pass.

## Setup

```bash
# Required env (loaded by sandbox workers)
export SURREAL_PASSWORD=root    # matches docker-compose.yml surrealdb service
```

All queries go through Caddy on `http://localhost:8000/surreal/sql` with headers `surreal-ns: research` + `surreal-db: main` (SurrealDB v3.1+ requires lowercase header names — the uppercase `NS`/`DB` form was deprecated).

## Check 1 — transcripts_complete

**Goal:** every `item` of type `voice` or `video` has a `transcript` child with non-empty `text`. Target: 34/34 on the Telegram dataset.

```bash
# Count voice/video items missing a transcript OR with empty-text transcript.
# SurrealDB v3 syntax: array::len() on graph-traversed field.
# ->transcribed_by->transcript.text returns array of text values from linked transcripts.
MISSING=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d 'SELECT count() FROM item WHERE type IN ["voice", "video"] AND (count(->transcribed_by->transcript) = 0 OR array::len(->transcribed_by->transcript.text) = 0) GROUP ALL' \
    | jq -r '.[0].result[0].count')

TOTAL=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d 'SELECT count() FROM item WHERE type IN ["voice", "video"] GROUP ALL' \
    | jq -r '.[0].result[0].count')

echo "transcripts: $((TOTAL - MISSING))/$TOTAL"
```

**Pass:** MISSING = 0.
**Fail sample:** list first 5 missing IDs:
```bash
curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d 'SELECT id FROM item WHERE type IN ["voice", "video"] AND (count(->transcribed_by->transcript) = 0 OR array::len(->transcribed_by->transcript.text) = 0) LIMIT 5' \
    | jq -r '.[0].result[].id'
```

**Common failure cause:** ASR provider 429'd mid-batch. Re-delegate INGEST_AUDIO_DIARIZATION with the explicit list of missing IDs.

## Check 2 — sender_names_clean

**Goal:** zero `sender` values contain a timestamp. Target: 0.

```bash
# SurrealDB v3 regex: use string::matches() with double-escaped backslashes.
# Single quote outside, double backslash inside the SQL literal.
POLLUTED=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM item WHERE string::matches(sender, '\\\\d{2}\\\\.\\\\d{2}\\\\.\\\\d{4}'))" \
    | jq -r '.[0].result')
```

**Pass:** POLLUTED = 0.
**Common failure cause:** parser regex broken, or someone re-ingested via raw HTML scraping instead of `telegram_parser.py`.

## Check 3 — sender_extraction_worked

**Goal:** <5% of items have `sender='Unknown'`. Target: <36/720 on Telegram dataset.

```bash
UNKNOWN=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM item WHERE sender = 'Unknown')" \
    | jq -r '.[0].result')

TOTAL=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM item)" \
    | jq -r '.[0].result')

echo "unknown rate: $(echo "scale=2; $UNKNOWN * 100 / $TOTAL" | bc)%"
```

**Pass:** rate < 5%.
**Disambiguation:** Genuine `Unknown` (forwarded messages, deleted accounts) is fine. Sample 10 Unknown senders and check the raw HTML — if they're all forwarded messages, the parser is working correctly. If they're normal messages with visible `from_name` in HTML, the parser missed them.

## Check 4 — topic_discovery_ran

**Goal:** `topic` table has ≥5 rows. Target: ≥5.

```bash
TOPICS=$(curl -sX POST http://localhost:8000/surreal/sql \
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
PERSONS=$(curl -sX POST http://localhost:8000/surreal/sql \
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
LINKED=$(curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "RETURN count(SELECT id FROM person WHERE face_centroid != NONE AND voice_centroid != NONE)" \
    | jq -r '.[0].result')
```

**Pass:** LINKED ≥ 1.
**Note:** This check only passes if checks #5 ran first. If #5 fails, #6 trivially fails too — note this in the report and move on.

## Mutation test

Verify the auditor catches regression. Plant one of the original failure modes manually:

```bash
# Plant a polluted sender name
curl -sX POST http://localhost:8000/surreal/sql \
    -H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main" \
    -u "root:$SURREAL_PASSWORD" \
    -d "UPDATE item:voice_5 SET sender = 'Will  03.03.2026 16:45:33' WHERE id = item:voice_5"

# Re-run check #2 — must report POLLUTED = 1
```

If check #2 still returns 0 after planting bad data, the regex check is broken.

## When all 6 pass

Report `overall: pass`. The CTO can now safely delegate to the artifact-generation phase (research-director + artifact-director) knowing the underlying knowledge graph is trustworthy.

If any check still fails after 5 iteration rounds, escalate to the user with the concrete failure mode + proposed fix. Don't keep retrying the same thing hoping it works.
