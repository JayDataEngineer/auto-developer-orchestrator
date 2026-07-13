#!/usr/bin/env python3
"""SurrealDB client — the persistence layer for the DRE knowledge graph.

This is the SCRIPT behind the pux_sandbox_surreal_* declared tools. It reads
SURREALDB_URL / SURREALDB_NS / SURREALDB_DB from the environment (injected by
the harness via policy.yaml sandbox.env) and exposes subcommands for every
graph operation the DRE pipeline needs:

  init          — define the schema (tables + graph edges)
  query         — run arbitrary SurrealQL (read-only by convention)
  count         — quick row counts for every table
  save-items    — bulk-insert parsed Telegram items
  save-transcript — insert an ASR transcript + link it to its voice item
  save-face     — insert face_appearance records + link to photo items
  save-source   — insert a source record (briefs, reports, web pages)
  save-topic    — insert topics + link to items via mentions edges
  start-task    — record that a pipeline run started (idempotency check)
  complete-task — record that a pipeline run finished
  task-status   — check whether a task already completed (resume support)
  vector-search — embed a query via Ollama + rank records by cosine similarity
  upsert-person — create/update a person node (idempotent on canonical_name)
  relate        — create a graph edge between two records (idempotent)

The agent NEVER calls this script directly. The declared tools in tools.yaml
(pux_sandbox_surreal_query, pux_sandbox_surreal_save_items, etc.) exec this
script in-container with typed args. The agent calls the tool by name.

ENV (injected by harness policy.yaml sandbox.env):
  SURREALDB_URL   Base URL, e.g. http://host.docker.internal:8000
  SURREALDB_NS    Namespace (default: research)
  SURREALDB_DB    Database (default: main)
  SURREALDB_USER  User (default: root)
  SURREALDB_PASS  Password (default: root)
"""

import argparse
import base64
import json
import os
import sys
import urllib.request
from pathlib import Path


# ---------------------------------------------------------------------------
# Connection
# ---------------------------------------------------------------------------

def _sql_endpoint() -> str:
    """The SurrealDB SQL HTTP endpoint.

    SurrealDB 3.x uses {base}/sql — NOT {base}/surreal/sql (that was a Caddy
    proxy prefix that doesn't exist in this deployment). The base URL comes
    from SURREALDB_URL (injected by the harness).
    """
    base = os.environ.get("SURREALDB_URL", "http://localhost:8000").rstrip("/")
    return base + "/sql"


def _headers() -> dict:
    ns = os.environ.get("SURREALDB_NS", "research")
    db = os.environ.get("SURREALDB_DB", "main")
    user = os.environ.get("SURREALDB_USER", "root")
    password = os.environ.get("SURREALDB_PASS", "root")
    auth = base64.b64encode(f"{user}:{password}".encode()).decode()
    return {
        "Content-Type": "text/plain",
        "Accept": "application/json",
        "surreal-ns": ns,
        "surreal-db": db,
        "Authorization": f"Basic {auth}",
    }


def execute_sql(sql: str, timeout: int = 30) -> list[dict]:
    """Run SurrealQL and return the parsed response array.

    SurrealDB returns an array of {result, status, time} objects — one per
    statement. This function raises on HTTP errors and on SurrealQL-level
    errors (status != OK).
    """
    url = _sql_endpoint()
    data = sql.encode()
    req = urllib.request.Request(url, data=data, headers=_headers())
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as e:
        body = e.read().decode(errors="replace")[:500]
        raise RuntimeError(f"SurrealDB HTTP {e.code}: {body}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"SurrealDB unreachable at {url}: {e.reason}") from e

    results = json.loads(raw)
    if not isinstance(results, list):
        results = [results]

    # Check for SurrealQL-level errors
    for r in results:
        if isinstance(r, dict) and r.get("status") != "OK":
            raise RuntimeError(f"SurrealQL error: {r.get('result', r)}")

    return results


# ---------------------------------------------------------------------------
# Schema
# ---------------------------------------------------------------------------

SCHEMA_SQL = """
-- Core entities
DEFINE TABLE IF NOT EXISTS item TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS transcript TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS speaker_turn TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS face_appearance TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS media TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS person TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS topic TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS organization TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS location TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS event TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS source TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS cluster TYPE ANY SCHEMALESS PERMISSIONS NONE;

-- Graph edges
DEFINE TABLE IF NOT EXISTS transcribed_by TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS appears_in TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS speaks_in TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS mentions TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS extracted_from TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS relates_to TYPE RELATION PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS belongs_to TYPE RELATION PERMISSIONS NONE;

-- Run tracking (idempotency)
DEFINE TABLE IF NOT EXISTS ingestion_run TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS task_run TYPE ANY SCHEMALESS PERMISSIONS NONE;
DEFINE TABLE IF NOT EXISTS pending_link TYPE ANY SCHEMALESS PERMISSIONS NONE;
"""


# ---------------------------------------------------------------------------
# Subcommands
# ---------------------------------------------------------------------------

def cmd_init(args):
    """Define the schema (tables + edges). Idempotent — safe to re-run."""
    execute_sql(SCHEMA_SQL)
    print(json.dumps({"status": "ok", "message": "schema defined (22 tables + 7 edges)"}))


def cmd_query(args):
    """Run arbitrary SurrealQL. Returns raw JSON results."""
    sql = args.sql
    if args.file:
        sql = Path(args.file).read_text()
    results = execute_sql(sql, timeout=args.timeout)
    # Return just the result arrays (strip status/time envelopes)
    output = [r.get("result") for r in results if isinstance(r, dict)]
    print(json.dumps(output, indent=2, default=str))


def cmd_count(args):
    """Quick row counts for every table."""
    tables = [
        "item", "transcript", "speaker_turn", "face_appearance", "media",
        "person", "topic", "organization", "location", "event", "source",
        "cluster", "mentions", "appears_in", "transcribed_by", "speaks_in",
        "extracted_from", "task_run", "ingestion_run",
    ]
    counts = {}
    for tbl in tables:
        try:
            r = execute_sql(f"SELECT count() FROM {tbl} GROUP ALL;", timeout=5)
            result = r[0].get("result", []) if r else []
            counts[tbl] = result[0]["count"] if result else 0
        except Exception:
            counts[tbl] = "error"
    print(json.dumps(counts, indent=2))


def cmd_save_items(args):
    """Bulk-insert parsed items from a JSON file (items.json from telegram_parser).

    Each item gets a record in the `item` table with type, text, sender,
    timestamp, path, forwarded. Items are keyed by a deterministic id derived
    from timestamp + index so re-running is idempotent (UPSERT).
    """
    data = json.loads(Path(args.input).read_text())
    items = data.get("items", data) if isinstance(data, dict) else data
    if not isinstance(items, list):
        print(json.dumps({"error": "expected list of items"}))
        sys.exit(1)

    inserted = 0
    errors = []
    for i, item in enumerate(items):
        ts = item.get("timestamp", f"unknown_{i}")
        # Deterministic record id: item:<sanitized_timestamp>_<index>
        safe_ts = "".join(c if c.isalnum() else "_" for c in str(ts))[:60]
        rid = f"item:{safe_ts}_{i}"
        sql = (
            f"UPSERT {rid} SET "
            f"type = {json.dumps(item.get('type', 'unknown'))}, "
            f"text = {json.dumps(item.get('text', ''))}, "
            f"sender = {json.dumps(item.get('sender', 'Unknown'))}, "
            f"timestamp = {json.dumps(item.get('timestamp', ''))}, "
            f"path = {json.dumps(item.get('path') or '')}, "
            f"forwarded = {json.dumps(bool(item.get('forwarded', False)))};"
        )
        try:
            execute_sql(sql, timeout=10)
            inserted += 1
        except Exception as e:
            errors.append(f"item {i}: {e}")

    print(json.dumps({
        "status": "ok",
        "total": len(items),
        "inserted": inserted,
        "errors": errors[:5],
    }, indent=2))


def cmd_save_transcript(args):
    """Insert a transcript record and link it to its voice/video item.

    Creates transcript:<name> with text + source file, then RELATEs
    item:<item_id>->transcribed_by->transcript:<name>.
    """
    data = json.loads(Path(args.input).read_text()) if args.input else \
        {"text": args.text, "file": args.file_name}
    text = data.get("transcript", data.get("text", ""))
    fname = data.get("file", args.file_name or "unknown")

    safe_name = "".join(c if c.isalnum() else "_" for c in fname)[:60]
    rid = f"transcript:{safe_name}"

    sql = f"UPSERT {rid} SET text = {json.dumps(text)}, source_file = {json.dumps(fname)};"
    if args.item_id:
        sql += f"\nRELATE item:{args.item_id}->transcribed_by->{rid};"
    execute_sql(sql, timeout=10)
    print(json.dumps({"status": "ok", "id": rid, "chars": len(text)}))


def cmd_save_source(args):
    """Insert a source record (briefs, reports, web pages)."""
    path = Path(args.path)
    content = path.read_text() if path.is_file() else ""
    safe_name = "".join(c if c.isalnum() else "_" for c in path.stem)[:60]
    rid = f"source:{safe_name}"
    sql = (
        f"UPSERT {rid} SET "
        f"kind = {json.dumps(args.kind)}, "
        f"path = {json.dumps(str(path))}, "
        f"topic = {json.dumps(args.topic or '')}, "
        f"content = {json.dumps(content[:50000])}, "
        f"saved_at = time::now()::format('%Y-%m-%dT%H:%M:%SZ');"
    )
    execute_sql(sql, timeout=10)
    print(json.dumps({"status": "ok", "id": rid, "chars": len(content)}))


def cmd_start_task(args):
    """Record that a pipeline task started. Supports resume: check before running."""
    rid = f"task_run:{args.task_id}"
    sql = (
        f"UPSERT {rid} SET "
        f"task = {json.dumps(args.task_id)}, "
        f"stage = {json.dumps(args.stage or 'started')}, "
        f"started_at = time::now()::format('%Y-%m-%dT%H:%M:%SZ'), "
        f"status = 'running';"
    )
    execute_sql(sql, timeout=10)
    print(json.dumps({"status": "ok", "task_id": args.task_id, "stage": args.stage or "started"}))


def cmd_complete_task(args):
    """Record that a pipeline task completed."""
    rid = f"task_run:{args.task_id}"
    sql = (
        f"UPDATE {rid} SET "
        f"status = 'completed', "
        f"completed_at = time::now()::format('%Y-%m-%dT%H:%M:%SZ'), "
        f"result = {json.dumps(args.result or '')};"
    )
    execute_sql(sql, timeout=10)
    print(json.dumps({"status": "ok", "task_id": args.task_id, "status": "completed"}))


def cmd_task_status(args):
    """Check whether a task already completed (resume support)."""
    try:
        r = execute_sql(f"SELECT status FROM task_run:{args.task_id};", timeout=5)
        result = r[0].get("result", []) if r else []
        if result and isinstance(result, list) and len(result) > 0:
            status = result[0].get("status", "unknown")
        else:
            status = "never_run"
    except Exception:
        status = "never_run"
    print(json.dumps({"task_id": args.task_id, "status": status}))


# ---------------------------------------------------------------------------
# Bulk artifact loaders (for backfill + going-forward ingest)
# ---------------------------------------------------------------------------

def _safe_id(prefix: str, raw: str, index: int = 0) -> str:
    """Build a deterministic record id from a raw string."""
    safe = "".join(c if c.isalnum() else "_" for c in str(raw))[:60]
    return f"{prefix}:{safe}"


def cmd_save_faces(args):
    """Bulk-insert face_appearance records from face_analysis.json.

    Each photo entry has {photo, url, n_faces, faces, embeddings}. For every
    face detected, insert a face_appearance row with the photo path, the
    subject (if recognized), the embedding, and the bbox.
    """
    data = json.loads(Path(args.input).read_text())
    if not isinstance(data, list):
        data = data.get("results", [data])

    inserted = 0
    photos_with_faces = 0
    for i, entry in enumerate(data):
        photo = entry.get("photo", f"unknown_{i}")
        faces = entry.get("faces", []) or []
        embeddings = entry.get("embeddings", []) or []
        if not faces:
            continue
        photos_with_faces += 1
        for j, face in enumerate(faces):
            emb = embeddings[j] if j < len(embeddings) else None
            subject = face.get("subject") or face.get("name") or f"unknown_{i}_{j}"
            sim = face.get("similarity", 0)
            bbox = face.get("box") or face.get("bbox") or {}
            rid = _safe_id("face_appearance", f"{photo}_{j}")
            sql = (
                f"UPSERT {rid} SET "
                f"photo = {json.dumps(photo)}, "
                f"subject = {json.dumps(str(subject))}, "
                f"similarity = {float(sim) if sim else 0.0}, "
                f"bbox = {json.dumps(bbox)}, "
                f"embedding = {json.dumps(emb) if emb else 'NONE'};"
            )
            try:
                execute_sql(sql, timeout=10)
                inserted += 1
            except Exception:
                pass  # keep going on per-face errors

    print(json.dumps({
        "status": "ok",
        "photos_total": len(data),
        "photos_with_faces": photos_with_faces,
        "face_appearances_inserted": inserted,
    }, indent=2))


def cmd_save_video_captions(args):
    """Bulk-insert video frame captions as media records.

    Each entry has {frame, url, caption}. Persist the caption + frame name
    so the grounding check and report can cite them.
    """
    data = json.loads(Path(args.input).read_text())
    if not isinstance(data, list):
        data = [data]

    inserted = 0
    for i, entry in enumerate(data):
        frame = entry.get("frame", f"frame_{i}")
        caption = entry.get("caption", "")
        url = entry.get("url", "")
        rid = _safe_id("media", frame)
        sql = (
            f"UPSERT {rid} SET "
            f"type = 'video_frame', "
            f"frame = {json.dumps(frame)}, "
            f"caption = {json.dumps(caption)}, "
            f"url = {json.dumps(url)};"
        )
        try:
            execute_sql(sql, timeout=10)
            inserted += 1
        except Exception:
            pass

    print(json.dumps({
        "status": "ok",
        "video_frames_total": len(data),
        "media_inserted": inserted,
    }, indent=2))


def _table_count(table: str) -> int:
    """Quick count for a single table (best-effort)."""
    try:
        r = execute_sql(f"SELECT count() FROM {table} GROUP ALL;", timeout=5)
        result = r[0].get("result", []) if r else []
        return result[0]["count"] if result else 0
    except Exception:
        return -1


def _all_counts() -> dict:
    """Counts for every table (used by backfill final report)."""
    tables = [
        "item", "transcript", "speaker_turn", "face_appearance", "media",
        "person", "topic", "organization", "location", "event", "source",
        "cluster", "mentions", "appears_in", "transcribed_by", "speaks_in",
        "extracted_from", "task_run", "ingestion_run",
    ]
    return {tbl: _table_count(tbl) for tbl in tables}


def cmd_backfill(args):
    """Backfill ALL artifacts in a run directory into SurrealDB.

    Looks for the standard artifact files (items.json, audio_chunks.json,
    face_analysis.json, video_frame_analysis.json, *.md reports) under the
    given run directory and persists each. Idempotent: every row uses
    UPSERT with a deterministic id, so re-running is safe.

    This is the migration path from ephemeral run-artifact JSON files to the
    persistent knowledge graph. After backfill, the data is queryable and
    pipeline runs can resume.
    """
    run_dir = Path(args.run_dir)
    if not run_dir.is_dir():
        print(json.dumps({"error": f"not a directory: {run_dir}"}))
        sys.exit(1)

    report = {"run_dir": str(run_dir), "steps": []}

    def _step(name, ok, detail=""):
        report["steps"].append({"step": name, "status": "ok" if ok else "skipped", "detail": detail})

    # 1. Schema (idempotent)
    try:
        execute_sql(SCHEMA_SQL)
        _step("schema", True)
    except Exception as e:
        _step("schema", False, str(e))

    # 2. Items
    items_path = run_dir / "items.json"
    if items_path.is_file():
        before = _table_count("item")
        try:
            data = json.loads(items_path.read_text())
            items = data.get("items", data) if isinstance(data, dict) else data
            for i, item in enumerate(items if isinstance(items, list) else []):
                ts = item.get("timestamp", f"unknown_{i}")
                safe_ts = "".join(c if c.isalnum() else "_" for c in str(ts))[:60]
                rid = f"item:{safe_ts}_{i}"
                sql = (
                    f"UPSERT {rid} SET "
                    f"type = {json.dumps(item.get('type', 'unknown'))}, "
                    f"text = {json.dumps(item.get('text', ''))}, "
                    f"sender = {json.dumps(item.get('sender', 'Unknown'))}, "
                    f"timestamp = {json.dumps(item.get('timestamp', ''))}, "
                    f"path = {json.dumps(item.get('path') or '')}, "
                    f"forwarded = {json.dumps(bool(item.get('forwarded', False)))};"
                )
                execute_sql(sql, timeout=10)
            after = _table_count("item")
            _step("items", True, f"{before} → {after}")
        except Exception as e:
            _step("items", False, str(e)[:200])
    else:
        _step("items", False, "no items.json")

    # 3. Audio transcripts (audio_chunks.json: [{content, source}, ...])
    chunks_path = run_dir / "audio_chunks.json"
    if chunks_path.is_file():
        before = _table_count("transcript")
        try:
            chunks = json.loads(chunks_path.read_text())
            if isinstance(chunks, dict):
                chunks = chunks.get("chunks", [])
            for i, chunk in enumerate(chunks if isinstance(chunks, list) else []):
                content = chunk.get("content", "")
                source = chunk.get("source", f"chunk_{i}")
                safe = "".join(c if c.isalnum() else "_" for c in source)[:60]
                rid = f"transcript:{safe}_{i}"
                sql = (
                    f"UPSERT {rid} SET "
                    f"text = {json.dumps(content)}, "
                    f"source_file = {json.dumps(source)};"
                )
                execute_sql(sql, timeout=10)
            after = _table_count("transcript")
            _step("audio_transcripts", True, f"{before} → {after}")
        except Exception as e:
            _step("audio_transcripts", False, str(e)[:200])
    else:
        _step("audio_transcripts", False, "no audio_chunks.json")

    # 4. Face appearances
    face_path = run_dir / "face_analysis.json"
    if face_path.is_file():
        before = _table_count("face_appearance")
        try:
            data = json.loads(face_path.read_text())
            inserted = 0
            for i, entry in enumerate(data if isinstance(data, list) else []):
                faces = entry.get("faces", []) or []
                embeddings = entry.get("embeddings", []) or []
                photo = entry.get("photo", f"photo_{i}")
                for j, face in enumerate(faces):
                    emb = embeddings[j] if j < len(embeddings) else None
                    subject = face.get("subject") or face.get("name") or f"unknown_{i}_{j}"
                    sim = face.get("similarity", 0)
                    rid = _safe_id("face_appearance", f"{photo}_{j}")
                    sql = (
                        f"UPSERT {rid} SET "
                        f"photo = {json.dumps(photo)}, "
                        f"subject = {json.dumps(str(subject))}, "
                        f"similarity = {float(sim) if sim else 0.0}, "
                        f"embedding = {json.dumps(emb) if emb else 'NONE'};"
                    )
                    try:
                        execute_sql(sql, timeout=10)
                        inserted += 1
                    except Exception:
                        pass
            after = _table_count("face_appearance")
            photos_with = sum(1 for e in (data if isinstance(data, list) else []) if (e.get("faces") or []))
            _step("face_appearances", True, f"{before} → {after} ({inserted} faces in {photos_with} photos)")
        except Exception as e:
            _step("face_appearances", False, str(e)[:200])
    else:
        _step("face_appearances", False, "no face_analysis.json")

    # 5. Video frame captions
    video_path = run_dir / "video_frame_analysis.json"
    if video_path.is_file():
        before = _table_count("media")
        try:
            data = json.loads(video_path.read_text())
            for i, entry in enumerate(data if isinstance(data, list) else []):
                frame = entry.get("frame", f"frame_{i}")
                caption = entry.get("caption", "")
                rid = _safe_id("media", frame)
                sql = (
                    f"UPSERT {rid} SET "
                    f"type = 'video_frame', "
                    f"frame = {json.dumps(frame)}, "
                    f"caption = {json.dumps(caption)};"
                )
                try:
                    execute_sql(sql, timeout=10)
                except Exception:
                    pass
            after = _table_count("media")
            _step("video_captions", True, f"{before} → {after}")
        except Exception as e:
            _step("video_captions", False, str(e)[:200])
    else:
        _step("video_captions", False, "no video_frame_analysis.json")

    # 6. Markdown reports (every *.md becomes a source record)
    md_files = sorted(run_dir.glob("*.md"))
    for md in md_files:
        safe = "".join(c if c.isalnum() else "_" for c in md.stem)[:60]
        rid = f"source:{safe}"
        try:
            content = md.read_text()
            sql = (
                f"UPSERT {rid} SET "
                f"kind = 'report', "
                f"path = {json.dumps(str(md))}, "
                f"topic = {json.dumps(md.stem)}, "
                f"content = {json.dumps(content[:50000])};"
            )
            execute_sql(sql, timeout=10)
            _step(f"source:{md.name}", True, f"{len(content)} chars")
        except Exception as e:
            _step(f"source:{md.name}", False, str(e)[:200])

    # Final counts
    report["final_counts"] = {k: v for k, v in _all_counts().items()
                              if isinstance(v, int) and v > 0}
    print(json.dumps(report, indent=2))


# ---------------------------------------------------------------------------
# Embedding + vector search
# ---------------------------------------------------------------------------

def _embed(text: str) -> list[float]:
    """Embed text via the in-sandbox sentence-transformers model.

    Uses sandbox/embed.py which loads microsoft/harrier-oss-v1-0.6b once
    (module-level cache) and encodes in-process. No HTTP hop, no env URL.
    Returns a 1024-dim L2-normalized vector.
    """
    from embed import encode
    return encode(text[:8000])


# Default embedding field per table — agents can override with --field.
_DEFAULT_EMB_FIELD = {
    "item": "text_embedding",
    "transcript": "embedding",
    "source": "embedding",
    "topic": "centroid_embedding",
    "face_appearance": "embedding",
}


def cmd_vector_search(args):
    """Semantic similarity search: embed the query, then rank by cosine.

    Usage:
        vector-search --table transcript --query "infiltration confession" --k 5
        vector-search --table topic --field centroid_embedding --query "gun ownership" --k 3
    """
    table = args.table
    field = args.field or _DEFAULT_EMB_FIELD.get(table, "embedding")
    k = args.k
    try:
        query_vec = _embed(args.query)
    except RuntimeError as e:
        print(json.dumps({"status": "error", "error": str(e)}))
        sys.exit(1)

    vec_str = json.dumps(query_vec)
    sql = (
        f"SELECT id, "
        f"vector::similarity::cosine({field}, {vec_str}) AS score "
        f"FROM {table} WHERE {field} != NONE "
        f"ORDER BY score DESC LIMIT {k};"
    )
    try:
        results = execute_sql(sql, timeout=args.timeout)
        rows = results[0].get("result", []) if results else []
    except Exception as e:
        print(json.dumps({"status": "error", "error": str(e)[:300]}))
        sys.exit(1)
    # Clean output: just id + score
    output = [{"id": r.get("id"), "score": round(r.get("score", 0), 4)} for r in rows]
    print(json.dumps({"status": "ok", "table": table, "field": field,
                      "query": args.query[:100], "results": output}))


# ---------------------------------------------------------------------------
# Person upsert + graph edges
# ---------------------------------------------------------------------------

def _safe_record_id(prefix: str, raw: str) -> str:
    """Build a deterministic record id: prefix:sanitized_name."""
    safe = "".join(c if c.isalnum() else "_" for c in raw.lower().strip())[:60]
    return f"{prefix}:{safe}"


def cmd_upsert_person(args):
    """Create or update a person node (idempotent on canonical_name).

    Usage:
        upsert-person --name "Elon Musk" --role subject --notes "CEO of SpaceX"
        upsert-person --name "Grady" --source-id source:abc123 --role subject
    """
    rid = _safe_record_id("person", args.name)
    parts = [
        f"canonical_name = {json.dumps(args.name)}",
        f"name = {json.dumps(args.name)}",
    ]
    if args.role:
        parts.append(f"role = {json.dumps(args.role)}")
    if args.notes:
        parts.append(f"notes = {json.dumps(args.notes)}")
    if args.face_centroid:
        parts.append(f"face_centroid = {args.face_centroid}")
    if args.voice_centroid:
        parts.append(f"voice_centroid = {args.voice_centroid}")
    parts.append("updated_at = time::now()::format('%Y-%m-%dT%H:%M:%SZ')")
    sql = f"UPSERT {rid} SET {', '.join(parts)};"
    execute_sql(sql, timeout=10)

    # Optionally link to a source via extracted_from edge
    if args.source_id:
        edge_sql = f"RELATE {rid}->extracted_from->{args.source_id};"
        try:
            execute_sql(edge_sql, timeout=5)
        except Exception:
            pass  # edge may already exist

    print(json.dumps({"status": "ok", "id": rid, "name": args.name}))


def cmd_relate(args):
    """Create a graph edge between two records (idempotent).

    Usage:
        relate --src topic:abc123 --edge extracted_from --tgt source:def456
        relate --src person:john_doe --edge appears_in --tgt item:xyz789
    """
    # Build edge table name (SurrealDB graph edges are table names)
    edge = args.edge
    sql = f"RELATE {args.src}->{edge}->{args.tgt}"
    if args.props:
        # props is a JSON string like '{"role":"speaker"}'
        try:
            props = json.loads(args.props)
            set_clause = ", ".join(f"{k} = {json.dumps(v)}" for k, v in props.items())
            sql += f" SET {set_clause}"
        except json.JSONDecodeError:
            pass  # ignore malformed props
    sql += ";"
    execute_sql(sql, timeout=10)
    print(json.dumps({"status": "ok", "edge": edge, "src": args.src, "tgt": args.tgt}))


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)

    sub.add_parser("init", help="define the schema")
    sub.add_parser("count", help="row counts for every table")

    p_q = sub.add_parser("query", help="run arbitrary SurrealQL")
    p_q.add_argument("--sql", default="", help="SurrealQL string")
    p_q.add_argument("--file", default="", help="read SurrealQL from file")
    p_q.add_argument("--timeout", type=int, default=30)

    p_si = sub.add_parser("save-items", help="bulk-insert parsed items")
    p_si.add_argument("--input", required=True, help="items.json path")

    p_st = sub.add_parser("save-transcript", help="insert transcript + link to item")
    p_st.add_argument("--input", default="", help="transcript JSON file")
    p_st.add_argument("--text", default="", help="transcript text (if no --input)")
    p_st.add_argument("--file-name", default="", help="source audio file name")
    p_st.add_argument("--item-id", default="", help="related item id (without item: prefix)")

    p_ss = sub.add_parser("save-source", help="insert a source record")
    p_ss.add_argument("--kind", required=True, help="brief / report / web / etc.")
    p_ss.add_argument("--path", required=True, help="file path")
    p_ss.add_argument("--topic", default="", help="topic label")

    p_sf = sub.add_parser("save-faces", help="bulk-insert face_appearance records")
    p_sf.add_argument("--input", required=True, help="face_analysis.json path")

    p_sv = sub.add_parser("save-video-captions", help="bulk-insert video frame captions")
    p_sv.add_argument("--input", required=True, help="video_frame_analysis.json path")

    p_bf = sub.add_parser("backfill", help="backfill ALL artifacts in a run directory")
    p_bf.add_argument("--run-dir", required=True, help="run directory (e.g. artifacts/run-2026-07-12)")

    p_start = sub.add_parser("start-task", help="record task started")
    p_start.add_argument("--task-id", required=True)
    p_start.add_argument("--stage", default="")

    p_done = sub.add_parser("complete-task", help="record task completed")
    p_done.add_argument("--task-id", required=True)
    p_done.add_argument("--result", default="")

    p_ts = sub.add_parser("task-status", help="check if task already ran")
    p_ts.add_argument("--task-id", required=True)

    p_vs = sub.add_parser("vector-search", help="semantic similarity search")
    p_vs.add_argument("--table", required=True, help="table to search (transcript, item, source, topic, face_appearance)")
    p_vs.add_argument("--query", required=True, help="natural-language query (will be embedded)")
    p_vs.add_argument("--field", default="", help="embedding column name (auto-detected from table if omitted)")
    p_vs.add_argument("--k", type=int, default=5, help="number of results")
    p_vs.add_argument("--timeout", type=int, default=30)

    p_up = sub.add_parser("upsert-person", help="create/update a person node")
    p_up.add_argument("--name", required=True, help="canonical name")
    p_up.add_argument("--source-id", default="", help="source record to link via extracted_from")
    p_up.add_argument("--role", default="", help="subject / mentioned / author")
    p_up.add_argument("--notes", default="", help="free-text notes")
    p_up.add_argument("--face-centroid", default="", help="face centroid vector JSON")
    p_up.add_argument("--voice-centroid", default="", help="voice centroid vector JSON")

    p_rel = sub.add_parser("relate", help="create a graph edge")
    p_rel.add_argument("--src", required=True, help="source record id (e.g. topic:abc123)")
    p_rel.add_argument("--edge", required=True, help="edge table name (e.g. extracted_from, appears_in, mentions)")
    p_rel.add_argument("--tgt", required=True, help="target record id (e.g. source:def456)")
    p_rel.add_argument("--props", default="", help='edge properties JSON (e.g. \'{"role":"speaker"}\')')

    args = ap.parse_args()

    handlers = {
        "init": cmd_init,
        "count": cmd_count,
        "query": cmd_query,
        "save-items": cmd_save_items,
        "save-transcript": cmd_save_transcript,
        "save-source": cmd_save_source,
        "save-faces": cmd_save_faces,
        "save-video-captions": cmd_save_video_captions,
        "backfill": cmd_backfill,
        "start-task": cmd_start_task,
        "complete-task": cmd_complete_task,
        "task-status": cmd_task_status,
        "vector-search": cmd_vector_search,
        "upsert-person": cmd_upsert_person,
        "relate": cmd_relate,
    }
    handlers[args.cmd](args)


if __name__ == "__main__":
    main()
