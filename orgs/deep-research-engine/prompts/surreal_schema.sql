-- ============================================================================
-- Deep Research Org — SurrealDB Context Engine Schema (v2)
-- ============================================================================
-- Canonical schema. Loaded by `surreal_client.py init-schema`.
-- Idempotent — DEFINE IF NOT EXISTS makes re-runs safe.
--
-- Design:
--   * item — one row per Telegram message (text/voice/photo/video). Replaces
--            the v1 "media" table as the primary ingestion unit.
--   * transcript + speaker_turn — audio analysis (Parakeet ASR + VAD + WeSpeaker)
--   * face_appearance — face detection + embeddings (InsightFace buffalo_l)
--   * person — identity cluster. May have face_centroid[512], voice_centroid[256],
--              or both (proves cross-modal linking). Cluster IDs are scoped:
--              face clusters use person_fc_N, voice-only use person_vc_N.
--   * pending_link — multimodal deferrals (multi-face ambiguous cases)
--
-- Edge conventions:
--   transcribed_by:  media|item  -> transcript
--   appears_in:      person      -> item        (role: photographed|recorded_in)
--   speaks_in:       person      -> item        (role: speaker|off_camera_speaker)
--   mentions:        topic       -> item
-- ============================================================================

-- --- Entities ---------------------------------------------------------------
DEFINE TABLE IF NOT EXISTS item;              -- Telegram message (text/voice/photo/video)
DEFINE TABLE IF NOT EXISTS person;            -- Identity cluster
DEFINE TABLE IF NOT EXISTS organization;
DEFINE TABLE IF NOT EXISTS location;
DEFINE TABLE IF NOT EXISTS topic;             -- Discovered theme (LLM-extracted)
DEFINE TABLE IF NOT EXISTS event;
DEFINE TABLE IF NOT EXISTS media;             -- Legacy alias for item (back-compat)
DEFINE TABLE IF NOT EXISTS source;            -- Provenance: every entity knows where it came from
DEFINE TABLE IF NOT EXISTS cluster;           -- Thematic grouping
DEFINE TABLE IF NOT EXISTS transcript;        -- ASR output, first-class (linked to item/media)
DEFINE TABLE IF NOT EXISTS speaker_turn;      -- VAD segment within a transcript; has voice_embedding[256]
DEFINE TABLE IF NOT EXISTS face_appearance;   -- Face detection within an item; has embedding[512]
DEFINE TABLE IF NOT EXISTS pending_link;      -- Multimodal deferrals (multi-face ambiguous cases)
DEFINE TABLE IF NOT EXISTS ingestion_run;     -- Tracks each pipeline run (started_at, models, stats)
DEFINE TABLE IF NOT EXISTS task_run;          -- Cross-session task history (CTO loop writes one per user request)

-- --- Relations (TYPE RELATION, no FROM/TO constraint in v3) -----------------
DEFINE TABLE IF NOT EXISTS mentions TYPE RELATION;        -- topic -> item
DEFINE TABLE IF NOT EXISTS appears_in TYPE RELATION;      -- person -> item
DEFINE TABLE IF NOT EXISTS speaks_in TYPE RELATION;       -- person -> item
DEFINE TABLE IF NOT EXISTS transcribed_by TYPE RELATION;  -- item|media -> transcript
DEFINE TABLE IF NOT EXISTS belongs_to TYPE RELATION;      -- item|transcript -> cluster
DEFINE TABLE IF NOT EXISTS extracted_from TYPE RELATION;  -- entity -> source
DEFINE TABLE IF NOT EXISTS relates_to TYPE RELATION;      -- person -> person (same_as, alias_of)

-- --- Vector indexes (HNSW, v3 syntax) ---------------------------------------
-- Face embeddings (InsightFace buffalo_l / MobileFaceNet-22k): 512-dim
-- Voice embeddings (WeSpeaker CAM++): 256-dim
-- Text embeddings (Jina v5 / OpenAI text-embedding-3-small): 1024-dim
-- If you change embedding model, drop + recreate the index.

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
DEFINE INDEX IF NOT EXISTS idx_item_text_embedding
    ON item FIELDS text_embedding HNSW DIMENSION 1024;
DEFINE INDEX IF NOT EXISTS idx_source_embedding
    ON source FIELDS embedding HNSW DIMENSION 1024;

-- --- Convenience indexes for common lookups ---------------------------------
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

-- --- task_run indexes (cross-session task history) --------------------------
DEFINE INDEX IF NOT EXISTS idx_task_run_started ON task_run FIELDS started_at;
DEFINE INDEX IF NOT EXISTS idx_task_run_status ON task_run FIELDS status;
DEFINE INDEX IF NOT EXISTS idx_task_run_prompt ON task_run FIELDS prompt;

-- ============================================================================
-- Auditor queries (6 success criteria) — see skills/AUDIT_QUALITY_GATES.md
-- ============================================================================
-- 1. Empty transcripts:  SELECT count() FROM item WHERE type='voice' AND !->transcript.text GROUP ALL
-- 2. Sender pollution:   SELECT count() FROM item WHERE sender ~ '\d{2}\.\d{2}\.\d{4}'
-- 3. Unknown rate:       SELECT count() FROM item WHERE sender='Unknown'
-- 4. Topic count:        SELECT count() FROM topic GROUP ALL
-- 5. Person count:       SELECT count() FROM person GROUP ALL
-- 6. Cross-modal linked: SELECT count() FROM person WHERE face_centroid != NONE AND voice_centroid != NONE
