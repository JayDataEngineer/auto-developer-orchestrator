#!/usr/bin/env python3
"""
SurrealDB client for the deep-research org.

Replaces neo4j_client.py + vector_search.py with a single client against a
single SurrealDB instance. SurrealDB gives us graph + relational + document +
vector in one binary, one connection, one query language (SurrealQL).

Design:
  - HTTP client (not the official surrealdb Python driver). HTTP matches the
    pattern in face_client.py (requests → CompreFace) and avoids async
    complexity. The /sql endpoint accepts SurrealQL strings.
  - Auth: Basic auth with user/pass (default root:root).
  - Namespace/Database: configurable via env vars.
  - Embeddings: optional. Calls an OpenAI-compatible /v1/embeddings endpoint.
    Required only for `embed` and `vector-search` subcommands.

Usage:
  python3 surreal_client.py init-schema
  python3 surreal_client.py insert --table person --input '{"name":"Test"}'
  python3 surreal_client.py select --table person --where "name CONTAINS 'Test'"
  python3 surreal_client.py query --q "SELECT * FROM person LIMIT 5"
  python3 surreal_client.py relate --src person:abc --edge appears_in --tgt media:xyz
  python3 surreal_client.py embed --text "hello"
  python3 surreal_client.py vector-search --table transcript --query "..." --k 5
  python3 surreal_client.py schema
  python3 surreal_client.py stats

Env vars:
  SURREALDB_URL     default http://localhost:8000/surreal
                    (Caddy strips /surreal prefix and forwards to surrealdb:8000
                     inside the docker network. To bypass Caddy, set this to
                     http://localhost:8000 directly with `ports:` published.)
  SURREALDB_NS      default research
  SURREALDB_DB      default main
  SURREALDB_USER    default root
  SURREALDB_PASS    default root
  EMBEDDING_API_URL default http://localhost:11434/v1  (Ollama, OpenAI-compatible)
  EMBEDDING_MODEL   default mxbai-embed-large  (1024-dim, matches schema)
  EMBEDDING_API_KEY default empty (set if provider requires)

Exit codes:
  0  success
  1  connection / query error
  2  bad CLI args
  3  missing env var or prerequisite
"""
import argparse
import base64
import json
import os
import sys
from typing import Any, Optional

try:
    import requests
except ImportError:
    print(json.dumps({
        "error": "requests not installed. Add 'requests' to pux.yaml pip_packages.",
    }))
    sys.exit(3)


def _to_surrealql(v: Any) -> str:
    """Serialize a Python value as a SurrealQL literal.

    Used to inline bound params into SQL strings (because the Caddy /surreal/sql
    proxy doesn't honor {"sql":..., "vars":...} JSON bodies).

    Handles: str, int, float, bool, None, list, dict. For anything else, falls
    back to str(v) which is usually wrong but at least visible.
    """
    if v is None:
        return "NONE"
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (int, float)):
        return repr(v)
    if isinstance(v, str):
        # SurrealQL double-quoted strings: escape backslash + double-quote.
        # Single quotes inside don't need escaping when string is double-quoted.
        escaped = v.replace("\\", "\\\\").replace('"', '\\"')
        return f'"{escaped}"'
    if isinstance(v, (list, tuple)):
        return "[" + ", ".join(_to_surrealql(x) for x in v) + "]"
    if isinstance(v, dict):
        return "{" + ", ".join(f"{k}: {_to_surrealql(val)}" for k, val in v.items()) + "}"
    return _to_surrealql(str(v))


# ----------------------------------------------------------------------------
# Connection config
# ----------------------------------------------------------------------------

class SurrealClient:
    def __init__(self) -> None:
        self.url = os.environ.get("SURREALDB_URL", "http://localhost:8000/surreal").rstrip("/")
        self.ns = os.environ.get("SURREALDB_NS", "research")
        self.db = os.environ.get("SURREALDB_DB", "main")
        self.user = os.environ.get("SURREALDB_USER", "root")
        self.password = os.environ.get("SURREALDB_PASS", "root")
        # Sanity check the URL has a scheme
        if "://" not in self.url:
            self.url = f"http://{self.url}"

    def _headers(self, *, sql: bool = False, with_context: bool = True) -> dict[str, str]:
        h: dict[str, str] = {
            "Accept": "application/json",
        }
        if with_context:
            # NS/DB headers tell SurrealDB which namespace + database to USE.
            # If we send these before the namespace/database exist, queries fail.
            h["NS"] = self.ns
            h["DB"] = self.db
            # Also send v2-style headers (SurrealDB accepts both)
            h["surreal-ns"] = self.ns
            h["surreal-db"] = self.db
        if self.user:
            token = base64.b64encode(f"{self.user}:{self.password}".encode()).decode()
            h["Authorization"] = f"Basic {token}"
        if sql:
            # /sql endpoint wants raw SurrealQL, not JSON
            h["Content-Type"] = "application/octet-stream"
        else:
            h["Content-Type"] = "application/json"
        return h

    def query_root(self, sql: str) -> Any:
        """Run SurrealQL WITHOUT NS/DB context. Used to create namespaces + databases."""
        try:
            r = requests.post(
                f"{self.url}/sql",
                data=sql.encode("utf-8"),
                headers=self._headers(sql=True, with_context=False),
                timeout=60,
            )
        except requests.exceptions.ConnectionError as e:
            print(json.dumps({"error": f"cannot reach SurrealDB at {self.url}: {e}"}))
            sys.exit(1)
        if r.status_code >= 400:
            print(json.dumps({
                "error": f"SurrealDB HTTP {r.status_code}",
                "body": r.text[:500],
            }))
            sys.exit(1)
        data = r.json()
        if isinstance(data, list):
            for stmt in data:
                if isinstance(stmt, dict) and stmt.get("status") == "ERR":
                    print(json.dumps({
                        "error": "SurrealQL error",
                        "detail": stmt.get("result"),
                    }))
                    sys.exit(1)
            if len(data) == 1 and isinstance(data[0], dict):
                return data[0].get("result")
        return data

    # ------------------------------------------------------------------
    # Core query primitive — everything funnels through this
    # ------------------------------------------------------------------

    def query(self, sql: str, params: Optional[dict] = None) -> Any:
        """Run raw SurrealQL. Returns the parsed JSON response.

        SurrealDB's /sql endpoint always returns a JSON array of result objects
        (one per statement), each with `status` and `result` keys.
        For convenience we extract `.result` when there's a single statement.

        NOTE: bound params ($foo) are inlined as SurrealQL literals before sending
        because the Caddy /surreal/sql proxy does not honor the {"sql":..., "vars":...}
        JSON body format. Values are serialized via _to_surrealql() which handles
        str/int/float/bool/None/list/dict.
        """
        if params:
            # Sort keys longest-first so $content doesn't partially match $content_len
            for k in sorted(params.keys(), key=len, reverse=True):
                sql = sql.replace(f"${k}", _to_surrealql(params[k]))
        body = sql
        try:
            r = requests.post(
                f"{self.url}/sql",
                data=body.encode("utf-8"),
                headers=self._headers(sql=True),
                timeout=60,
            )
        except requests.exceptions.ConnectionError as e:
            print(json.dumps({"error": f"cannot reach SurrealDB at {self.url}: {e}"}))
            sys.exit(1)

        if r.status_code >= 400:
            print(json.dumps({
                "error": f"SurrealDB HTTP {r.status_code}",
                "body": r.text[:500],
            }))
            sys.exit(1)

        try:
            data = r.json()
        except ValueError:
            print(json.dumps({"error": "SurrealDB returned non-JSON response", "body": r.text[:500]}))
            sys.exit(1)

        # Check for SurrealQL-level errors
        if isinstance(data, list):
            for stmt in data:
                if isinstance(stmt, dict) and stmt.get("status") == "ERR":
                    print(json.dumps({
                        "error": "SurrealQL error",
                        "detail": stmt.get("result"),
                    }))
                    sys.exit(1)
            # Unwrap: if single statement, return .result directly
            if len(data) == 1 and isinstance(data[0], dict):
                return data[0].get("result")
        return data

    # ------------------------------------------------------------------
    # CRUD (table-based — uses /table/{t} REST endpoints)
    # ------------------------------------------------------------------

    def insert(self, table: str, records: Any) -> Any:
        """Insert one record (dict) or many (list). Returns created records.

        Uses CREATE ... CONTENT via /sql endpoint (v3 doesn't expose /table/{t}).
        """
        payload = records if isinstance(records, list) else [records]
        sql_parts = []
        for r in payload:
            sql_parts.append(f"CREATE {table} CONTENT {json.dumps(r)}")
        sql = ";\n".join(sql_parts)
        return self.query(sql)

    def select(self, table: str, where: Optional[str] = None, limit: Optional[int] = None) -> Any:
        """Select all (or filtered) records from a table."""
        sql = f"SELECT * FROM {table}"
        if where:
            sql += f" WHERE {where}"
        if limit:
            sql += f" LIMIT {limit}"
        return self.query(sql)

    def update(self, table: str, record_id: str, fields: dict) -> Any:
        """Merge-update a single record."""
        sql = f"UPDATE {table}:{record_id} MERGE {json.dumps(fields)}"
        return self.query(sql)

    def delete(self, table: str, record_id: str) -> Any:
        return self.query(f"DELETE FROM {table}:{record_id}")

    def relate(self, src: str, edge_type: str, tgt: str, props: Optional[dict] = None) -> Any:
        """Create a relation: src -[edge_type]-> tgt with optional properties.

        `src` and `tgt` are full record refs like 'person:abc123'.
        """
        props_clause = f" CONTENT {json.dumps(props)}" if props else ""
        sql = f"RELATE {src}->{edge_type}->{tgt}{props_clause}"
        return self.query(sql)

    def neighbors(self, node_ref: str, edge_type: Optional[str] = None, direction: str = "both") -> Any:
        """Find edges/neighbors of a node.

        Direction:
          - 'out': edges where this node is the source (`in` column = node)
          - 'in':  edges where this node is the target (`out` column = node)
          - 'both': union of both

        We query the relation table directly rather than using graph-traversal
        operators (which changed syntax in v3). This is version-stable.

        Returns edge records with `id`, `in`, `out`, plus any edge properties.
        Caller follows `out`/`in` IDs to get the neighbor records.
        """
        edge_tables = [edge_type] if edge_type else [
            "mentions", "appears_in", "belongs_to", "extracted_from", "relates_to",
        ]
        results = {"out": [], "in": []}
        for tbl in edge_tables:
            if direction in ("out", "both"):
                q = f"SELECT * FROM {tbl} WHERE in = {node_ref}"
                rows = self.query(q)
                if isinstance(rows, list):
                    results["out"].extend([{"edge": tbl, **r} for r in rows])
            if direction in ("in", "both"):
                q = f"SELECT * FROM {tbl} WHERE out = {node_ref}"
                rows = self.query(q)
                if isinstance(rows, list):
                    results["in"].extend([{"edge": tbl, **r} for r in rows])
        return results

    # ------------------------------------------------------------------
    # Embeddings (OpenAI-compatible API)
    # ------------------------------------------------------------------

    def embed(self, text: str) -> list[float]:
        api_url = os.environ.get("EMBEDDING_API_URL", "http://localhost:11434/v1")
        api_url = api_url.rstrip("/")
        if not api_url.endswith("/embeddings"):
            api_url = f"{api_url}/embeddings"
        api_key = os.environ.get("EMBEDDING_API_KEY", "")
        model = os.environ.get("EMBEDDING_MODEL", "mxbai-embed-large")
        try:
            r = requests.post(
                api_url,
                json={"input": text, "model": model},
                headers={"Authorization": f"Bearer {api_key}"} if api_key else {},
                timeout=30,
            )
        except requests.exceptions.ConnectionError as e:
            print(json.dumps({"error": f"cannot reach embedding API at {api_url}: {e}"}))
            sys.exit(1)
        r.raise_for_status()
        data = r.json()
        # OpenAI-compatible response: {data: [{embedding: [...]}]}
        return data["data"][0]["embedding"]

    def vector_search(
        self,
        table: str,
        field: str,
        query_vec: list[float],
        k: int = 10,
        threshold: Optional[float] = None,
    ) -> Any:
        """Similarity search. Requires a vector index on (table, field).

        Uses the `<|vec|>` KNN prefix to trigger index-accelerated search
        in SurrealDB v2. Falls back to a scan if no index present.
        """
        # The /sql endpoint wants raw SurrealQL, not {"sql":..., "vars":...} JSON.
        # So serialize the query vector inline as a SurrealQL array literal.
        # Safe because the values are floats (no injection risk).
        arr = "[" + ",".join(repr(float(x)) for x in query_vec) + "]"
        sql = (
            f"SELECT id, vector::similarity::cosine({field}, {arr}) AS score "
            f"FROM {table} WHERE {field} != NONE "
        )
        if threshold is not None:
            sql += f"AND vector::similarity::cosine({field}, {arr}) >= {threshold} "
        sql += f"ORDER BY score DESC LIMIT {k}"
        r = requests.post(
            f"{self.url}/sql",
            data=sql.encode("utf-8"),
            headers=self._headers(sql=True),
            timeout=60,
        )
        r.raise_for_status()
        data = r.json()
        # /sql returns an array of {status, result}; unwrap the single statement
        if isinstance(data, list) and len(data) == 1 and isinstance(data[0], dict):
            return data[0].get("result")
        return data


# ----------------------------------------------------------------------------
# Schema definition
# ----------------------------------------------------------------------------

SCHEMA_SQL = """
-- ============================================================================
-- Deep Research Org — SurrealDB Context Engine Schema (v2)
-- ============================================================================
-- Canonical source: profiles/deep-research-engine/prompts/surreal_schema.sql
-- Loaded by init_schema() via _load_schema_sql() with this as fallback.
-- ============================================================================

-- --- Entities -----------------------------------------------------------
DEFINE TABLE IF NOT EXISTS item;              -- Telegram message (text/voice/photo/video)
DEFINE TABLE IF NOT EXISTS person;
DEFINE TABLE IF NOT EXISTS organization;
DEFINE TABLE IF NOT EXISTS location;
DEFINE TABLE IF NOT EXISTS topic;
DEFINE TABLE IF NOT EXISTS event;
DEFINE TABLE IF NOT EXISTS media;             -- Legacy alias for item (back-compat)
DEFINE TABLE IF NOT EXISTS source;
DEFINE TABLE IF NOT EXISTS cluster;
DEFINE TABLE IF NOT EXISTS transcript;
DEFINE TABLE IF NOT EXISTS speaker_turn;      -- VAD segment within a transcript
DEFINE TABLE IF NOT EXISTS face_appearance;   -- Face detection within an item
DEFINE TABLE IF NOT EXISTS pending_link;      -- Multimodal deferrals
DEFINE TABLE IF NOT EXISTS ingestion_run;     -- Tracks each pipeline run (started_at, models, stats)
DEFINE TABLE IF NOT EXISTS task_run;          -- Cross-session task history (CTO writes one per user request)

-- --- Relations ---------------------------------------------------------
DEFINE TABLE IF NOT EXISTS mentions TYPE RELATION;
DEFINE TABLE IF NOT EXISTS appears_in TYPE RELATION;
DEFINE TABLE IF NOT EXISTS speaks_in TYPE RELATION;
DEFINE TABLE IF NOT EXISTS transcribed_by TYPE RELATION;
DEFINE TABLE IF NOT EXISTS belongs_to TYPE RELATION;
DEFINE TABLE IF NOT EXISTS extracted_from TYPE RELATION;
DEFINE TABLE IF NOT EXISTS relates_to TYPE RELATION;

-- --- Vector indexes -----------------------------------------------------
-- Face=512-dim, Voice=256-dim, Text=1024-dim. Drop + recreate if model changes.

DEFINE INDEX IF NOT EXISTS idx_face_appearance_embedding
    ON face_appearance FIELDS embedding HNSW DIMENSION 512;
DEFINE INDEX IF NOT EXISTS idx_speaker_turn_voice_embedding
    ON speaker_turn FIELDS voice_embedding HNSW DIMENSION 256;
DEFINE INDEX IF NOT EXISTS idx_person_face_centroid
    ON person FIELDS face_centroid HNSW DIMENSION 512;
DEFINE INDEX IF NOT EXISTS idx_person_voice_centroid
    ON person FIELDS voice_centroid HNSW DIMENSION 256;
DEFINE INDEX IF NOT EXISTS idx_transcript_embedding
    ON transcript FIELDS embedding HNSW DIMENSION 1024;
DEFINE INDEX IF NOT EXISTS idx_topic_centroid_embedding
    ON topic FIELDS centroid_embedding HNSW DIMENSION 1024;

-- --- Convenience indexes ------------------------------------------------
DEFINE INDEX IF NOT EXISTS idx_item_type ON item FIELDS type;
DEFINE INDEX IF NOT EXISTS idx_item_sender ON item FIELDS sender;
DEFINE INDEX IF NOT EXISTS idx_item_timestamp ON item FIELDS timestamp;
DEFINE INDEX IF NOT EXISTS idx_person_name ON person FIELDS canonical_name;
DEFINE INDEX IF NOT EXISTS idx_source_path ON source FIELDS path;
DEFINE INDEX IF NOT EXISTS idx_media_sha256 ON media FIELDS sha256;
DEFINE INDEX IF NOT EXISTS idx_cluster_name ON cluster FIELDS name;
DEFINE INDEX IF NOT EXISTS idx_speaker_turn_cluster ON speaker_turn FIELDS voice_cluster_id;
DEFINE INDEX IF NOT EXISTS idx_face_appearance_cluster ON face_appearance FIELDS cluster_id;
DEFINE INDEX IF NOT EXISTS idx_face_appearance_item ON face_appearance FIELDS item_id;
DEFINE INDEX IF NOT EXISTS idx_speaker_turn_transcript ON speaker_turn FIELDS transcript_id;

-- task_run indexes (cross-session task history)
DEFINE INDEX IF NOT EXISTS idx_task_run_started ON task_run FIELDS started_at;
DEFINE INDEX IF NOT EXISTS idx_task_run_status ON task_run FIELDS status;
"""


def _load_schema_sql() -> str:
    """Prefer org's prompts/surreal_schema.sql over the inline fallback.

    Resolution order:
      1. $PUX_ORG_PATH/prompts/surreal_schema.sql  (if --org was used)
      2. ../profiles/deep-research-engine/prompts/surreal_schema.sql  (dev install)
      3. Inline SCHEMA_SQL constant (always available)
    """
    from pathlib import Path

    candidates = []
    env_org = os.environ.get("PUX_ORG_PATH")
    if env_org:
        candidates.append(Path(env_org) / "prompts" / "surreal_schema.sql")
    try:
        candidates.append(Path(__file__).resolve().parents[2] / "profiles" / "deep-research-engine" / "prompts" / "surreal_schema.sql")
    except IndexError:
        pass

    for p in candidates:
        if p.is_file():
            return p.read_text()
    return SCHEMA_SQL


def init_schema(client: SurrealClient) -> dict[str, Any]:
    """Apply namespace + database + schema. Idempotent (DEFINE is safe to re-run).

    Two phases:
      1. Root context (no NS/DB): create namespace + database
      2. NS/DB context: create tables, edges, indexes
    """
    # Phase 1: namespace + database (requires root, no NS/DB context).
    # USE statement switches context mid-query so DEFINE DATABASE knows
    # which namespace to create the database in.
    # IF NOT EXISTS makes this idempotent. Backtick-escape NS/DB names so
    # hyphenated identifiers (e.g. "game-studio") aren't parsed as subtraction.
    root_sql = (
        f"DEFINE NAMESPACE IF NOT EXISTS `{client.ns}`;\n"
        f"USE NS `{client.ns}`;\n"
        f"DEFINE DATABASE IF NOT EXISTS `{client.db}`;\n"
    )
    client.query_root(root_sql)
    # Phase 2: schema (with NS/DB context)
    result = client.query(_load_schema_sql())
    return {
        "ok": True,
        "url": client.url,
        "ns": client.ns,
        "db": client.db,
        "statements_applied": "see query result",
        "result": result,
    }


def show_schema(client: SurrealClient) -> dict[str, Any]:
    """Inspect the current schema (tables + indexes)."""
    tables = client.query("INFO FOR DB")
    return {
        "ns": client.ns,
        "db": client.db,
        "info": tables,
    }


def show_stats(client: SurrealClient) -> dict[str, Any]:
    """Per-table row counts. Quick health check."""
    table_names = [
        "person", "organization", "location", "topic", "event",
        "media", "source", "cluster", "transcript",
        "mentions", "appears_in", "belongs_to", "extracted_from", "relates_to",
        "task_run", "ingestion_run",
    ]
    counts = {}
    for t in table_names:
        try:
            result = client.query(f"SELECT count() FROM {t} GROUP ALL")
            # SurrealDB returns [{count: N}] for count() GROUP ALL
            if isinstance(result, list) and result:
                counts[t] = result[0].get("count", 0)
            else:
                counts[t] = 0
        except SystemExit:
            counts[t] = "error"
    return {
        "ns": client.ns,
        "db": client.db,
        "counts": counts,
    }


# ----------------------------------------------------------------------------
# CLI
# ----------------------------------------------------------------------------

def read_input(value: str) -> Any:
    """Parse JSON input. Accepts:
      - JSON string inline (starts with '{' or '[')
      - '-' for stdin
      - file path
    """
    value = value.strip()
    if value.startswith("{") or value.startswith("["):
        return json.loads(value)
    if value == "-":
        return json.load(sys.stdin)
    with open(value) as f:
        return json.load(f)


def cmd_init_schema(args, client) -> None:
    print(json.dumps(init_schema(client), indent=2, default=str))


def cmd_save_source(args, client) -> None:
    """Create a source record with embedded content + optional extracted_from edges.

    This is the canonical write-path for web-researcher and pdf-ingestor. It:
      1. Embeds the content (via Ollama / mxbai-embed-large)
      2. INSERTs a source record (idempotent on URL/path — re-runs UPDATE)
      3. Optionally RELATEs topic_ids / person_ids → extracted_from → source

    Output: {"source_id": "source:xxx", "edges_created": N, "embedding_dim": 1024}
    """
    content = args.content
    if args.content_file:
        with open(args.content_file) as f:
            content = f.read()
    if not content.strip():
        print(json.dumps({"error": "content is empty (use --content or --content-file)"}))
        sys.exit(2)

    # 1. Embed
    try:
        embedding = client.embed(content[:8000])  # cap at 8k chars for speed
    except Exception as e:
        print(json.dumps({"error": f"embedding failed: {e}"}))
        sys.exit(1)

    # 2. Find existing source by URL/path (idempotent upsert)
    lookup_field = "url" if args.url else ("path" if args.path else None)
    lookup_value = args.url or args.path
    existing_id = None
    if lookup_field:
        rows = client.select(
            "source",
            where=f"{lookup_field} = '{lookup_value.replace(chr(39), chr(39)+chr(39))}'",
            limit=1,
        )
        if rows:
            existing_id = str(rows[0]["id"]).split(":", 1)[1]

    # 3. Build record fields — schemaless, so any field is allowed
    fields = {
        "kind": args.kind,
        "name": args.title or args.url or args.path or "untitled",
        "url": args.url,
        "path": args.path,
        "title": args.title,
        "author": args.author,
        "published_at": args.published_at,
        "accessed_at": args.accessed_at,
        "content": content[:16000],  # cap stored content
        "content_len": len(content),
        "embedding": embedding,
        "embedding_dim": len(embedding),
        "ingested_at": "time::now()",
    }
    # Strip None values to keep the record clean
    fields = {k: v for k, v in fields.items() if v is not None}

    if existing_id:
        client.update("source", existing_id, fields)
        source_id = f"source:{existing_id}"
    else:
        # INSERT RETURN id — SurrealDB generates the record id
        result = client.query(
            "CREATE source SET " + ", ".join(f"{k} = ${k}" for k in fields) + " RETURN id",
            fields,
        )
        if isinstance(result, list) and result and isinstance(result[0], dict):
            source_id = str(result[0].get("id"))
        else:
            source_id = str(result)

    # 4. Create extracted_from edges
    edges_created = 0
    for tid in (args.topic_ids or []):
        client.relate(tid, "extracted_from", source_id)
        edges_created += 1
    for pid in (args.person_ids or []):
        client.relate(pid, "extracted_from", source_id)
        edges_created += 1

    print(json.dumps({
        "source_id": source_id,
        "edges_created": edges_created,
        "embedding_dim": len(embedding),
        "content_stored": len(content[:16000]),
    }, indent=2))


def cmd_upsert_person(args, client) -> None:
    """Find-or-create a person by canonical_name. Optionally link to a source.

    Use this when web-research/pdf-ingest encounters a named individual who isn't
    already in the DB. Idempotent on canonical_name (exact match — case-sensitive).

    Output: {"person_id": "person:xxx", "created": true|false, "edges_created": N}
    """
    name = args.name.strip()
    if not name:
        print(json.dumps({"error": "--name is required and non-empty"}))
        sys.exit(2)

    # 1. Lookup by exact canonical_name match
    rows = client.select(
        "person",
        where=f"canonical_name = {_to_surrealql(name)}",
        limit=1,
    )
    if rows:
        person_id = str(rows[0]["id"])
        created = False
    else:
        # 2. Create new person record
        fields = {
            "canonical_name": name,
            "discovered_via": args.discovered_via or "web_research",
            "first_seen": "PLACEHOLDER_TIME_NOW",
        }
        if args.role:
            fields["role_hint"] = args.role
        if args.notes:
            fields["notes"] = args.notes
        # Build SQL with time::now() inline (not as a string param)
        set_clauses = []
        for k, v in fields.items():
            if v == "PLACEHOLDER_TIME_NOW":
                set_clauses.append(f"{k} = time::now()")
            else:
                set_clauses.append(f"{k} = ${k}")
        # Remove placeholder from params
        fields = {k: v for k, v in fields.items() if v != "PLACEHOLDER_TIME_NOW"}
        sql = "CREATE person SET " + ", ".join(set_clauses) + " RETURN id"
        result = client.query(sql, fields if fields else None)
        if isinstance(result, list) and result and isinstance(result[0], dict):
            person_id = str(result[0].get("id"))
        else:
            person_id = str(result)
        created = True

    # 3. Optionally link to a source via extracted_from edge
    edges_created = 0
    if args.source_id:
        client.relate(person_id, "extracted_from", args.source_id)
        edges_created += 1

    print(json.dumps({
        "person_id": person_id,
        "created": created,
        "edges_created": edges_created,
    }, indent=2))


def cmd_start_task(args, client) -> None:
    """Open a task_run record at the start of a CTO task.

    The CTO calls this immediately after receiving a user request. The returned
    task_id is passed to complete-task when the work is done.

    Output: {"task_id": "task_run:xxx", "status": "running"}
    """
    prompt = args.prompt.strip()
    if not prompt:
        print(json.dumps({"error": "--prompt is required and non-empty"}))
        sys.exit(2)

    # time::now() inline (not a string param — we want a datetime, not a string)
    sql = (
        "CREATE task_run SET "
        "prompt = $prompt, "
        f"mode = {_to_surrealql(args.mode or '')}, "
        "started_at = time::now(), "
        "status = 'running', "
        "delegated_to = [], "
        "artifacts_produced = [], "
        "source_ids_created = [] "
        "RETURN id"
    )
    result = client.query(sql, {"prompt": prompt})
    if isinstance(result, list) and result and isinstance(result[0], dict):
        task_id = str(result[0].get("id"))
    else:
        task_id = str(result)
    print(json.dumps({"task_id": task_id, "status": "running"}, indent=2))


def cmd_complete_task(args, client) -> None:
    """Close out a task_run record at task completion.

    Updates the record with delegated_to, artifacts, source_ids, status, summary.
    If the task_id is for a different table (not task_run), parses accordingly.

    Output: {"task_id": "task_run:xxx", "status": "...", "completed": true}
    """
    task_ref = args.id
    # Allow both "task_run:xxx" and just "xxx" (assume task_run)
    if ":" in task_ref:
        table, record_id = task_ref.split(":", 1)
    else:
        table, record_id = "task_run", task_ref

    # Build SET clause — MERGE wants an object literal, SET accepts k=v assignments
    # which is what we need (allows time::now() inline + $param substitution)
    set_parts = ["completed_at = time::now()", f"status = {_to_surrealql(args.status)}"]
    params: dict[str, Any] = {}
    if args.delegated_to:
        params["delegated_to"] = list(args.delegated_to)
        set_parts.append("delegated_to = $delegated_to")
    if args.artifacts:
        params["artifacts_produced"] = list(args.artifacts)
        set_parts.append("artifacts_produced = $artifacts_produced")
    if args.source_ids:
        params["source_ids_created"] = list(args.source_ids)
        set_parts.append("source_ids_created = $source_ids_created")
    if args.summary:
        params["summary"] = args.summary
        set_parts.append("summary = $summary")

    sql = f"UPDATE {table}:{record_id} SET {', '.join(set_parts)}"
    client.query(sql, params if params else None)
    print(json.dumps({
        "task_id": f"{table}:{record_id}",
        "status": args.status,
        "completed": True,
    }, indent=2))


def cmd_schema(args, client) -> None:
    print(json.dumps(show_schema(client), indent=2, default=str))


def cmd_stats(args, client) -> None:
    print(json.dumps(show_stats(client), indent=2, default=str))


def cmd_query(args, client) -> None:
    result = client.query(args.q)
    print(json.dumps(result, indent=2, default=str))


def cmd_insert(args, client) -> None:
    records = read_input(args.input)
    result = client.insert(args.table, records)
    print(json.dumps(result, indent=2, default=str))


def cmd_select(args, client) -> None:
    result = client.select(args.table, where=args.where, limit=args.limit)
    print(json.dumps(result, indent=2, default=str))


def cmd_update(args, client) -> None:
    fields = read_input(args.input)
    result = client.update(args.table, args.id, fields)
    print(json.dumps(result, indent=2, default=str))


def cmd_delete(args, client) -> None:
    result = client.delete(args.table, args.id)
    print(json.dumps(result, indent=2, default=str))


def cmd_relate(args, client) -> None:
    props = read_input(args.props) if args.props else None
    result = client.relate(args.src, args.edge, args.tgt, props)
    print(json.dumps(result, indent=2, default=str))


def cmd_neighbors(args, client) -> None:
    result = client.neighbors(args.node, edge_type=args.edge, direction=args.direction)
    print(json.dumps(result, indent=2, default=str))


def cmd_embed(args, client) -> None:
    vec = client.embed(args.text)
    print(json.dumps({"dimensions": len(vec), "preview": vec[:8]}, indent=2))


def cmd_vector_search(args, client) -> None:
    qvec = client.embed(args.query)
    result = client.vector_search(args.table, args.field, qvec, k=args.k, threshold=args.threshold)
    print(json.dumps(result, indent=2, default=str))


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="SurrealDB client for deep-research org")
    sub = p.add_subparsers(dest="cmd", required=True)

    sub.add_parser("init-schema", help="Create tables, edges, indexes (idempotent)").set_defaults(func=cmd_init_schema)
    sub.add_parser("schema", help="Show current schema (INFO FOR DB)").set_defaults(func=cmd_schema)
    sub.add_parser("stats", help="Per-table row counts").set_defaults(func=cmd_stats)

    q = sub.add_parser("query", help="Run raw SurrealQL")
    q.add_argument("--q", required=True, help="SurrealQL statement")
    q.set_defaults(func=cmd_query)

    ins = sub.add_parser("insert", help="Insert records (JSON)")
    ins.add_argument("--table", required=True)
    ins.add_argument("--input", required=True, help="JSON file or '-' for stdin")
    ins.set_defaults(func=cmd_insert)

    sel = sub.add_parser("select", help="Select records")
    sel.add_argument("--table", required=True)
    sel.add_argument("--where", help="SurrealQL WHERE clause (without the WHERE keyword)")
    sel.add_argument("--limit", type=int)
    sel.set_defaults(func=cmd_select)

    upd = sub.add_parser("update", help="Merge-update a record")
    upd.add_argument("--table", required=True)
    upd.add_argument("--id", required=True, help="Record ID (without table prefix)")
    upd.add_argument("--input", required=True, help="JSON file or '-' for stdin")
    upd.set_defaults(func=cmd_update)

    dele = sub.add_parser("delete", help="Delete a record")
    dele.add_argument("--table", required=True)
    dele.add_argument("--id", required=True)
    dele.set_defaults(func=cmd_delete)

    rel = sub.add_parser("relate", help="Create a relation edge")
    rel.add_argument("--src", required=True, help="Full record ref, e.g. person:abc")
    rel.add_argument("--edge", required=True, help="Edge type, e.g. appears_in")
    rel.add_argument("--tgt", required=True, help="Full record ref, e.g. media:xyz")
    rel.add_argument("--props", help="JSON file of edge properties (optional)")
    rel.set_defaults(func=cmd_relate)

    nb = sub.add_parser("neighbors", help="Get neighbors of a node")
    nb.add_argument("--node", required=True, help="Full record ref")
    nb.add_argument("--edge", help="Edge type filter (optional)")
    nb.add_argument("--direction", choices=["in", "out", "both"], default="both")
    nb.set_defaults(func=cmd_neighbors)

    emb = sub.add_parser("embed", help="Embed text via OpenAI-compatible API")
    emb.add_argument("--text", required=True)
    emb.set_defaults(func=cmd_embed)

    vs = sub.add_parser("vector-search", help="Embed query then similarity search")
    vs.add_argument("--table", required=True)
    vs.add_argument("--field", default="embedding")
    vs.add_argument("--query", required=True, help="Query text")
    vs.add_argument("--k", type=int, default=10)
    vs.add_argument("--threshold", type=float)
    vs.set_defaults(func=cmd_vector_search)

    # save-source — atomic write path for web-researcher / pdf-ingestor
    ss = sub.add_parser("save-source", help="Write a source record with content + embedding + extracted_from edges")
    ss.add_argument("--kind", required=True, help="web | pdf | file | telegram | ...")
    ss.add_argument("--url", help="Source URL (for web sources)")
    ss.add_argument("--path", help="Source file path (for PDFs/local files)")
    ss.add_argument("--title", help="Title of the source")
    ss.add_argument("--author", help="Author of the source")
    ss.add_argument("--published-at", dest="published_at", help="Publication date (ISO 8601)")
    ss.add_argument("--accessed-at", dest="accessed_at", help="Access date (defaults to today via caller)")
    ss.add_argument("--content", help="Source content text (use --content-file for long text)")
    ss.add_argument("--content-file", dest="content_file", help="Path to a file with the source content")
    ss.add_argument("--topic-ids", dest="topic_ids", nargs="*", default=[], help="Record IDs (topic:xxx) to RELATE via extracted_from")
    ss.add_argument("--person-ids", dest="person_ids", nargs="*", default=[], help="Record IDs (person:xxx) to RELATE via extracted_from")
    ss.set_defaults(func=cmd_save_source)

    # upsert-person — find-or-create a person by canonical_name
    up = sub.add_parser("upsert-person", help="Find-or-create a person by canonical_name; optionally link to a source")
    up.add_argument("--name", required=True, help="Canonical name (e.g. 'Elon Musk'). Case-sensitive exact match.")
    up.add_argument("--source-id", dest="source_id", help="Source record ID (source:xxx) to RELATE via extracted_from")
    up.add_argument("--role", help="Role hint: 'speaker' | 'subject' | 'quoted' | 'mentioned'")
    up.add_argument("--notes", help="Free-text notes about this person")
    up.add_argument("--discovered-via", dest="discovered_via", help="How discovered (default: web_research)")
    up.set_defaults(func=cmd_upsert_person)

    # start-task — open a task_run record at start of a CTO task
    st = sub.add_parser("start-task", help="Open a task_run record (call at start of every CTO task)")
    st.add_argument("--prompt", required=True, help="The user's verbatim request")
    st.add_argument("--mode", help="Task mode (lightning | base)")
    st.set_defaults(func=cmd_start_task)

    # complete-task — close out a task_run record
    ct = sub.add_parser("complete-task", help="Close out a task_run record (call at end of every CTO task)")
    ct.add_argument("--id", required=True, help="task_run:xxx (or just the record ID)")
    ct.add_argument("--delegated-to", dest="delegated_to", nargs="*", default=[], help="Role names delegated to")
    ct.add_argument("--artifacts", nargs="*", default=[], help="Artifact file paths produced")
    ct.add_argument("--source-ids", dest="source_ids", nargs="*", default=[], help="source:xxx IDs created during task")
    ct.add_argument("--status", required=True, help="completed | failed | abandoned")
    ct.add_argument("--summary", help="1-2 sentence outcome")
    ct.set_defaults(func=cmd_complete_task)

    return p


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    client = SurrealClient()
    args.func(args, client)


if __name__ == "__main__":
    main()
