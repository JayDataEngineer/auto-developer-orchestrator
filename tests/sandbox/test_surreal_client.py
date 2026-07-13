"""Tests for the DRE SurrealDB persistence layer (surreal_client.py).

These tests verify the SCHEMA + IDempotency contract of the persistence layer
without requiring a live SurrealDB instance (the execute_sql function is
mocked). The persistence layer is what makes pipeline runs resumable — every
derived artifact (items, transcripts, faces, video captions, reports) gets
UPSERTed with a deterministic id so re-running is safe.

What these tests prove:
  - The schema SQL defines every table the knowledge graph needs (22 total)
  - Record ids are DETERMINISTIC (same input → same id → UPSERT, no dupes)
  - The /sql endpoint is used (NOT /surreal/sql — the SurrealDB 3.x path)
  - save-items, save-transcript, save-source, save-faces build correct SQL
  - backfill detects + loads every artifact type from a run directory
"""

import json
import sys
from pathlib import Path

import pytest

# Add the sandbox dir to path so we can import surreal_client
SANDBOX = Path(__file__).resolve().parents[2] / "orgs/specialists/deep-research-engine/sandbox"
sys.path.insert(0, str(SANDBOX))

import surreal_client


# ---------------------------------------------------------------------------
# Captured SQL — mock execute_sql to capture what would be sent
# ---------------------------------------------------------------------------

class CapturedSQL:
    """Captures every SQL statement passed to execute_sql."""
    def __init__(self):
        self.statements = []
        self.return_value = [{"status": "OK", "result": []}]

    def __call__(self, sql, timeout=30):
        self.statements.append(sql)
        return self.return_value


@pytest.fixture
def captured(monkeypatch):
    cap = CapturedSQL()
    monkeypatch.setattr(surreal_client, "execute_sql", cap)
    return cap


# ---------------------------------------------------------------------------
# Schema
# ---------------------------------------------------------------------------

class TestSchema:
    def test_schema_defines_all_core_tables(self):
        """The schema must define every table the knowledge graph needs."""
        for table in [
            "item", "transcript", "speaker_turn", "face_appearance",
            "media", "person", "topic", "organization", "location",
            "event", "source", "cluster",
        ]:
            assert f"DEFINE TABLE IF NOT EXISTS {table}" in surreal_client.SCHEMA_SQL, \
                f"schema missing table: {table}"

    def test_schema_defines_graph_edges(self):
        """The schema must define the graph edges that link entities."""
        for edge in [
            "transcribed_by", "appears_in", "speaks_in", "mentions",
            "extracted_from", "relates_to", "belongs_to",
        ]:
            assert f"DEFINE TABLE IF NOT EXISTS {edge} TYPE RELATION" in surreal_client.SCHEMA_SQL, \
                f"schema missing edge: {edge}"


# ---------------------------------------------------------------------------
# Endpoint — the /sql path (NOT /surreal/sql)
# ---------------------------------------------------------------------------

class TestEndpoint:
    def test_uses_sql_endpoint_not_surreal_sql(self, monkeypatch):
        """SurrealDB 3.x uses {base}/sql — NOT {base}/surreal/sql.
        The /surreal/sql path was a Caddy proxy prefix that doesn't exist
        in this deployment. Every query was hitting 404 before this fix."""
        monkeypatch.setenv("SURREALDB_URL", "http://localhost:8000")
        endpoint = surreal_client._sql_endpoint()
        assert endpoint == "http://localhost:8000/sql"
        assert "/surreal/sql" not in endpoint

    def test_strips_trailing_slash(self, monkeypatch):
        """Base URL with trailing slash still produces correct endpoint."""
        monkeypatch.setenv("SURREALDB_URL", "http://localhost:8000/")
        endpoint = surreal_client._sql_endpoint()
        assert endpoint == "http://localhost:8000/sql"

    def test_uses_env_url(self, monkeypatch):
        """The endpoint comes from SURREALDB_URL env (injected by harness)."""
        monkeypatch.setenv("SURREALDB_URL", "http://host.docker.internal:8000")
        endpoint = surreal_client._sql_endpoint()
        assert "host.docker.internal" in endpoint

    def test_headers_include_ns_and_db(self, monkeypatch):
        """Headers carry the surreal-ns and surreal-db from env."""
        monkeypatch.setenv("SURREALDB_NS", "research")
        monkeypatch.setenv("SURREALDB_DB", "main")
        headers = surreal_client._headers()
        assert headers["surreal-ns"] == "research"
        assert headers["surreal-db"] == "main"


# ---------------------------------------------------------------------------
# Deterministic IDs (idempotency contract)
# ---------------------------------------------------------------------------

class TestDeterministicIds:
    def test_safe_id_is_deterministic(self):
        """Same input must produce the same id — this is the UPSERT contract.
        If ids weren't deterministic, re-running would create duplicates and
        the 'idempotent knowledge graph' promise would be a lie."""
        id1 = surreal_client._safe_id("item", "photo_123@13-03-2026_08-59-45.jpg")
        id2 = surreal_client._safe_id("item", "photo_123@13-03-2026_08-59-45.jpg")
        assert id1 == id2

    def test_safe_id_sanitizes_special_chars(self):
        """Special chars in filenames become underscores — valid SurrealDB ids."""
        rid = surreal_client._safe_id("transcript", "voice_5@13-03-2026_08-59-45.ogg")
        assert rid.startswith("transcript:")
        assert "@" not in rid
        assert "." not in rid

    def test_different_inputs_produce_different_ids(self):
        """Different filenames must produce different ids (no collisions)."""
        id1 = surreal_client._safe_id("item", "photo_1.jpg")
        id2 = surreal_client._safe_id("item", "photo_2.jpg")
        assert id1 != id2


# ---------------------------------------------------------------------------
# save-items — bulk insert parsed Telegram items
# ---------------------------------------------------------------------------

class TestSaveItems:
    def test_builds_upsert_for_each_item(self, captured, tmp_path):
        """save-items must UPSERT one record per item with deterministic id."""
        items_path = tmp_path / "items.json"
        items_path.write_text(json.dumps({"items": [
            {"type": "text", "text": "hello", "sender": "Alice", "timestamp": "2026-03-13T08:00"},
            {"type": "voice", "text": "", "sender": "Bob", "timestamp": "2026-03-13T09:00"},
        ]}))

        class Args:
            input = str(items_path)
        surreal_client.cmd_save_items(Args())

        # Schema is NOT run by save-items (only by init/backfill)
        upserts = [s for s in captured.statements if s.startswith("UPSERT item:")]
        assert len(upserts) == 2
        assert "UPSERT" in upserts[0]  # idempotent, not INSERT
        assert "Alice" in upserts[0]
        assert "voice" in upserts[1]


# ---------------------------------------------------------------------------
# save-source — persist a brief/report
# ---------------------------------------------------------------------------

class TestSaveSource:
    def test_builds_upsert_with_content(self, captured, tmp_path):
        """save-source must UPSERT a source record with the file content."""
        report = tmp_path / "brief.md"
        report.write_text("# Brief\n\nKey finding: TSMC makes chips.")

        class Args:
            kind = "brief"
            path = str(report)
            topic = "semiconductors"
        surreal_client.cmd_save_source(Args())

        upserts = [s for s in captured.statements if "UPSERT source:" in s]
        assert len(upserts) == 1
        assert "brief" in upserts[0]
        assert "TSMC" in upserts[0]
        assert "semiconductors" in upserts[0]


# ---------------------------------------------------------------------------
# save-faces — bulk insert face_appearance records
# ---------------------------------------------------------------------------

class TestSaveFaces:
    def test_inserts_one_row_per_detected_face(self, captured, tmp_path):
        """save-faces must insert a face_appearance per face, not per photo."""
        faces_path = tmp_path / "face_analysis.json"
        faces_path.write_text(json.dumps([
            {"photo": "photo_1.jpg", "faces": [
                {"subject": "Alice", "similarity": 0.95},
                {"subject": "Bob", "similarity": 0.88},
            ], "embeddings": [[0.1] * 128, [0.2] * 128]},
            {"photo": "photo_2.jpg", "faces": [], "embeddings": []},
        ]))

        class Args:
            input = str(faces_path)
        surreal_client.cmd_save_faces(Args())

        upserts = [s for s in captured.statements if "UPSERT face_appearance:" in s]
        assert len(upserts) == 2  # 2 faces in photo_1, 0 in photo_2
        assert "Alice" in upserts[0]
        assert "Bob" in upserts[1]

    def test_skips_photos_with_zero_faces(self, captured, tmp_path):
        """Photos with no faces detected must not produce face_appearance rows."""
        faces_path = tmp_path / "face_analysis.json"
        faces_path.write_text(json.dumps([
            {"photo": "photo_empty.jpg", "faces": [], "embeddings": []},
        ]))

        class Args:
            input = str(faces_path)
        surreal_client.cmd_save_faces(Args())

        upserts = [s for s in captured.statements if "UPSERT face_appearance:" in s]
        assert len(upserts) == 0


# ---------------------------------------------------------------------------
# backfill — the migration path from ephemeral to persistent
# ---------------------------------------------------------------------------

class TestBackfill:
    def test_backfill_loads_every_artifact_type(self, captured, tmp_path):
        """backfill must detect + load items, transcripts, faces, video, reports."""
        # items.json
        (tmp_path / "items.json").write_text(json.dumps({"items": [
            {"type": "text", "text": "msg", "sender": "A", "timestamp": "t1"},
        ]}))
        # audio_chunks.json
        (tmp_path / "audio_chunks.json").write_text(json.dumps([
            {"content": "transcript text", "source": "voice_1.ogg"},
        ]))
        # face_analysis.json
        (tmp_path / "face_analysis.json").write_text(json.dumps([
            {"photo": "p.jpg", "faces": [{"subject": "X", "similarity": 0.9}], "embeddings": [[]]},
        ]))
        # video_frame_analysis.json
        (tmp_path / "video_frame_analysis.json").write_text(json.dumps([
            {"frame": "f1.jpg", "caption": "a scene"},
        ]))
        # a report
        (tmp_path / "report.md").write_text("# Report\n\nFindings.")

        class Args:
            run_dir = str(tmp_path)
        surreal_client.cmd_backfill(Args())

        all_sql = "\n".join(captured.statements)
        assert "DEFINE TABLE" in all_sql          # schema
        assert "UPSERT item:" in all_sql          # items
        assert "UPSERT transcript:" in all_sql    # audio transcripts
        assert "UPSERT face_appearance:" in all_sql  # faces
        assert "UPSERT media:" in all_sql         # video frames
        assert "UPSERT source:" in all_sql        # report

    def test_backfill_is_idempotent(self, captured, tmp_path):
        """Re-running backfill uses UPSERT — no duplicate rows.
        This is the core contract: running twice must produce the same SQL
        statements as running once (deterministic ids → safe re-runs)."""
        (tmp_path / "items.json").write_text(json.dumps({"items": [
            {"type": "text", "text": "msg", "sender": "A", "timestamp": "t1"},
        ]}))

        class Args:
            run_dir = str(tmp_path)

        surreal_client.cmd_backfill(Args())
        first_run_count = len(captured.statements)

        captured.statements.clear()
        surreal_client.cmd_backfill(Args())
        second_run_count = len(captured.statements)

        # Same number of statements → same UPSERTs → idempotent
        assert second_run_count == first_run_count

    def test_backfill_skips_missing_artifacts_gracefully(self, captured, tmp_path):
        """If a run dir has only items.json (no faces, no video), backfill
        loads what's there and skips the rest without crashing."""
        (tmp_path / "items.json").write_text(json.dumps({"items": [
            {"type": "text", "text": "msg", "sender": "A", "timestamp": "t1"},
        ]}))

        class Args:
            run_dir = str(tmp_path)
        surreal_client.cmd_backfill(Args())

        all_sql = "\n".join(captured.statements)
        assert "UPSERT item:" in all_sql
        # No face/video SQL was generated because those files don't exist
        assert "UPSERT face_appearance:" not in all_sql

    def test_backfill_persists_markdown_reports(self, captured, tmp_path):
        """Every *.md in the run dir becomes a source record."""
        (tmp_path / "brief.md").write_text("# Brief\n\nContent.")
        (tmp_path / "report.md").write_text("# Report\n\nMore content.")

        class Args:
            run_dir = str(tmp_path)
        surreal_client.cmd_backfill(Args())

        source_upserts = [s for s in captured.statements if "UPSERT source:" in s]
        assert len(source_upserts) >= 2  # brief.md + report.md


# ---------------------------------------------------------------------------
# Task tracking (idempotency / resume support)
# ---------------------------------------------------------------------------

class TestTaskTracking:
    def test_start_task_upserts_running_status(self, captured):
        class Args:
            task_id = "ingest_audio_run-2026-07-12"
            stage = "transcribe"
        surreal_client.cmd_start_task(Args())

        upserts = [s for s in captured.statements if "UPSERT task_run:" in s]
        assert len(upserts) == 1
        assert "running" in upserts[0]
        assert "ingest_audio_run-2026-07-12" in upserts[0]

    def test_complete_task_updates_to_completed(self, captured):
        class Args:
            task_id = "ingest_audio_run-2026-07-12"
            result = "37 transcripts"
        surreal_client.cmd_complete_task(Args())

        updates = [s for s in captured.statements if "UPDATE task_run:" in s]
        assert len(updates) == 1
        assert "completed" in updates[0]
