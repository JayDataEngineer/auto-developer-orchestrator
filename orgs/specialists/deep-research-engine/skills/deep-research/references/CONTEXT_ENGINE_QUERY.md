# CONTEXT_ENGINE_QUERY

**Read this before doing any ingestion work.** The world is not ephemeral — earlier runs have already processed data, and you can save time (and avoid duplicates) by querying the context engine first.

The SurrealDB context engine persists everything: items, transcripts, face/voice embeddings, persons, topics. Use the queries below to ask "what's already done?" before you start.

## How to query

EVERY query in this skill runs via the `mcp__surreal__query` tool. You call it by name — you never construct URLs, run curl, or manage auth. The tool handles the SurrealDB connection internally.

```
mcp__surreal__query(sql="<SurrealQL statement>")
```

For quick table counts: `mcp__surreal__query(sql="RETURN count(SELECT id FROM item)")`.

## What runs have happened?

The `ingestion_run` table tracks every pipeline run. Query it first.

```
mcp__surreal__query(sql="SELECT id, started_at, completed_at, source_path, pipeline_version, status, stats FROM ingestion_run ORDER BY started_at DESC")
```

## What tasks have we worked on? (cross-session history)

The `task_run` table records every CTO task — what the user asked, who the CTO delegated to, what artifacts were produced, and the outcome. Query this when the user asks "what did we do last week?" or "have we researched X before?"

```
# Recent tasks
mcp__surreal__query(sql="SELECT id, prompt, mode, started_at, completed_at, delegated_to, artifacts_produced, status, summary FROM task_run ORDER BY started_at DESC LIMIT 20")

# Tasks that produced an article
mcp__surreal__query(sql="SELECT prompt, started_at, summary FROM task_run WHERE 'artifacts/article.md' IN artifacts_produced")

# Still-running tasks (should usually be empty)
mcp__surreal__query(sql="SELECT prompt, started_at FROM task_run WHERE status = 'running'")
```

The CTO writes one `task_run` record per user request via `mcp__surreal__query` at the beginning and `mcp__surreal__query` at the end. Tasks that errored mid-loop show `status='running'` until manually cleaned up.

## Current state of the DB

Quick row counts for every table:

```
mcp__surreal__query(sql="RETURN count(SELECT id FROM item)")
```

## What's missing? (the auditor's questions)

Run the 7 audit queries from [[AUDIT_QUALITY_GATES]] — they pin down exactly what's broken or incomplete.

Quick: what voice/video items have NO transcript?

```
mcp__surreal__query(sql="SELECT id, type, sender, timestamp FROM item WHERE type IN ['voice','video'] AND count(->transcribed_by->transcript) = 0 LIMIT 20")
```

## What's already known about a person?

Cross-modal linking means a single `person` node can have both face and voice centroids. This is the "subject-matter expert" payoff.

```
# All persons with face AND voice linked (the gold standard)
mcp__surreal__query(sql="SELECT canonical_name, face_count, voice_count, face_centroid != NONE AS has_face, voice_centroid != NONE AS has_voice FROM person")

# Everything about a specific person (by name)
mcp__surreal__query(sql="SELECT *, ->appears_in->item.{type, timestamp, sender} AS photos, ->speaks_in->item.{type, timestamp, sender} AS recordings FROM person WHERE canonical_name CONTAINS 'Grady'")
```

## What's already known about a topic?

```
mcp__surreal__query(sql="SELECT name, keywords, summary, count(->mentions->item) AS mentions FROM topic ORDER BY mentions DESC")
```

## Decision rules

| Question | If answer is… | Action |
|----------|---------------|--------|
| Has anyone ingested `data/<export_dir>/`? | Yes, `status=completed` | Skip ingest. Read the existing rows. |
| Has anyone ingested it? | Yes, but `status=running` | Wait for completion. Don't start a parallel run. |
| Has anyone ingested it? | Yes, `status=failed` | Read `stats` to see how far it got, resume from there. |
| Has anyone ingested it? | No | Start a fresh ingestion. |
| All 7 audit checks pass? | Yes | Proceed to artifact generation (research-director, artifact-director). |
| All 7 audit checks pass? | No | Re-delegate to ingestion-director with the specific failed checks. |

## Re-ingest policies

The pipeline is **idempotent at the DB level** — every row uses UPSERT with a deterministic id, so re-running ingest produces the same state. No duplicates. This is the core design contract of the persistence layer.

1. **Full re-ingest** (for major model changes): delete the relevant tables, then re-run. The `mcp__surreal__query` tool can run DELETE statements.
   ```
   mcp__surreal__query(sql="DELETE FROM item; DELETE FROM transcript; DELETE FROM person; DELETE FROM speaker_turn; DELETE FROM face_appearance; DELETE FROM topic; DELETE FROM appears_in; DELETE FROM transcribed_by; DELETE FROM speaks_in; DELETE FROM mentions; DELETE FROM extracted_from;")
   ```
   File caches in the work_dir will speed up re-processing.

2. **Targeted re-ingest** (for partial failures): pass `--skip-audio` to skip transcription if those are cached, or delete only the failed item IDs and re-run.

## Why this matters

The original failure mode: the Python pipeline produced empty transcripts, "Unknown" senders, and zero topics — but **no one noticed for weeks** because nothing was checking. The context engine is the source of truth. If you don't query it before acting, you're flying blind.
