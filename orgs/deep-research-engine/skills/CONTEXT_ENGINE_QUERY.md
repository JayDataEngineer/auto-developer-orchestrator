# CONTEXT_ENGINE_QUERY

**Read this before doing any ingestion work.** The world is not ephemeral — earlier runs have already processed data, and you can save time (and avoid duplicates) by querying the context engine first.

The SurrealDB context engine persists everything: items, transcripts, face/voice embeddings, persons, topics. Use the queries below to ask "what's already done?" before you start.

## Setup

```bash
export SURREAL_PASSWORD=root
URL=http://localhost:8000/surreal/sql
HDR=(-H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main")
AUTH=-u "root:$SURREAL_PASSWORD"
```

## What runs have happened?

The `ingestion_run` table tracks every pipeline run. Query it first.

```bash
curl -sX POST $URL "${HDR[@]}" $AUTH \
    -d "SELECT id, started_at, completed_at, source_path, pipeline_version, status, stats FROM ingestion_run ORDER BY started_at DESC" \
    | jq -c '.[0].result[]'
```

Sample output:
```json
{
  "id": "ingestion_run:abc123",
  "started_at": "2026-06-19T02:09:14Z",
  "completed_at": "2026-06-19T02:51:33Z",
  "source_path": "data/ChatExport_2026-03-13/",
  "pipeline_version": "v2-onnx-2026-06-19",
  "status": "completed",
  "stats": {
    "items": 364,
    "transcripts": 47,
    "faces": 234,
    "persons": 12,
    "topics": 8,
    "cross_modal_linked": 3
  }
}
```

## Current state of the DB

Quick row counts for every table:

```bash
for T in item transcript speaker_turn face_appearance person topic pending_link ingestion_run; do
  COUNT=$(curl -sX POST $URL "${HDR[@]}" $AUTH -d "RETURN count(SELECT id FROM $T)" | jq -r '.[0].result')
  printf "%-20s %s\n" "$T" "$COUNT"
done
```

## What's missing? (the auditor's questions)

Run the 6 audit queries from [[AUDIT_QUALITY_GATES]] — they pin down exactly what's broken or incomplete.

Quick: what voice/video items have NO transcript?

```bash
curl -sX POST $URL "${HDR[@]}" $AUTH \
    -d 'SELECT id, type, sender, timestamp FROM item WHERE type IN ["voice","video"] AND count(->transcribed_by->transcript) = 0 LIMIT 20' \
    | jq -c '.[0].result[]'
```

## What's already known about a person?

Cross-modal linking means a single `person` node can have both face and voice centroids. This is the "subject-matter expert" payoff.

```bash
# All persons with face AND voice linked (the gold standard)
curl -sX POST $URL "${HDR[@]}" $AUTH \
    -d 'SELECT canonical_name, face_count, voice_count, face_centroid != NONE AS has_face, voice_centroid != NONE AS has_voice FROM person' \
    | jq -c '.[0].result[]'

# Everything about a specific person (by name)
curl -sX POST $URL "${HDR[@]}" $AUTH \
    -d "SELECT *, ->appears_in->item.{type, timestamp, sender} AS photos, ->speaks_in->item.{type, timestamp, sender} AS recordings FROM person WHERE canonical_name CONTAINS 'Grady'" \
    | jq -c '.[0].result[]'
```

## What's already known about a topic?

```bash
curl -sX POST $URL "${HDR[@]}" $AUTH \
    -d 'SELECT name, keywords, summary, count(->mentions->item) AS mentions FROM topic ORDER BY mentions DESC' \
    | jq -c '.[0].result[]'
```

## Decision rules

| Question | If answer is… | Action |
|----------|---------------|--------|
| Has anyone ingested `data/ChatExport_2026-03-13/`? | Yes, `status=completed` | Skip ingest. Read the existing rows. |
| Has anyone ingested it? | Yes, but `status=running` | Wait for completion. Don't start a parallel run. |
| Has anyone ingested it? | Yes, `status=failed` | Read `stats` to see how far it got, resume from there. |
| Has anyone ingested it? | No | Start a fresh ingestion. |
| All 6 audit checks pass? | Yes | Proceed to artifact generation (research-director, artifact-director). |
| All 6 audit checks pass? | No | Re-delegate to ingestion-director with the specific failed checks. |

## Re-ingest policies

The pipeline is **idempotent at the file level** (work_dir caches voice_N.json, photo_hash.json) but **NOT at the DB level** — re-running ingest inserts duplicate items. Two options:

1. **Wipe + replay** (recommended for major model changes):
   ```bash
   curl -sX POST $URL "${HDR[@]}" $AUTH -d "
   DELETE FROM item; DELETE FROM transcript; DELETE FROM person;
   DELETE FROM speaker_turn; DELETE FROM face_appearance; DELETE FROM topic;
   DELETE FROM appears_in; DELETE FROM transcribed_by; DELETE FROM speaks_in;
   DELETE FROM mentions; DELETE FROM extracted_from;
   "
   # Then re-run ingest. File caches in /tmp/ingest_v2_work will speed this up.
   ```

2. **Targeted re-ingest** (for partial failures): pass `--skip-audio` to skip transcription if those are cached, or delete only the failed item IDs and re-run.

## Why this matters

The original failure mode: the Python pipeline produced empty transcripts, "Unknown" senders, and zero topics — but **no one noticed for weeks** because nothing was checking. The context engine is the source of truth. If you don't query it before acting, you're flying blind.
