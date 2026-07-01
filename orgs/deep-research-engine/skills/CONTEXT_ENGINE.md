# Context Engine — Autonomous Ingestion → Intelligence Report

Standalone orchestrator that takes a raw multimodal corpus and produces a
structured intelligence report with zero human-in-the-loop decisions.

## When to use

- Structured chat/document export → intelligence analysis
- Bulk audio (interviews, voice memos, podcasts) → searchable knowledge graph
- Mixed media corpus (text + audio + images + video) → entity-rich narrative report
- Any task where "read everything, find the actors + themes, write it up" applies

## The CTO loop: ingest → audit → refine → re-ingest

The pipeline runs ingestion, then delegates to the **auditor** role, which
checks measurable success criteria. If any fail, the CTO re-delegates
ingestion with refined scope (max 5 rounds). See `skills/AUDIT_QUALITY_GATES.md`
for the full checklist.

The criteria, all must pass (goals are parametric — see audit skill for SQL):

1. Every `item` of type `voice`/`video` has a `transcript` child with non-empty `text`
2. Zero `sender` values match a timestamp-pollution regex
3. `sender='Unknown'` rate below threshold (parametric; disambiguates forwarded vs. parser-missed)
4. `topic` table meets minimum row count
5. Person clusters from faces+voices meet minimum distinct-count
6. Cross-modal linking: ≥1 `person` node has both `face_centroid` AND `voice_centroid`

## Pipeline

```
[1] Survey             →  count files by type (audio/image/text/video/other)
[2] Parse              →  format-specific parser in sandbox/ → items.json
                          (run `python3 sandbox/<parser>.py --help` to see supported formats)
[3] Audio              →  audio_client.py + voice_activity + embed_voice per file
                          (Parakeet ASR + Silero VAD + WeSpeaker embeddings + Pyannote diarization)
[4] Face clustering    →  INGEST_FACE_CLUSTERING_V2
                          (InsightFace SCRFD + MobileFaceNet + HDBSCAN)
[5] Cross-modal link   →  INGEST_MULTIMODAL_PERSONS
                          (lip-sync heuristic via temporal co-occurrence)
[6] Entities           →  entity_extract.py batch → entities.json
                          (LLM extracts people/organizations/topics/locations/dates per chunk)
[7] Knowledge Graph    →  surreal_client.py → SurrealDB
                          (item + transcript + speaker_turn + face_appearance + person + topic)
[8] Audit              →  AUDIT_QUALITY_GATES
                          (if any fail, re-delegate the responsible ingestion step)
[9] Synthesis          →  LLM reads whole corpus → intelligence_report.md
                          (structured markdown: actors, themes, timeline, warnings)
```

## Usage

```bash
# Full autonomous run on a corpus directory
python3 sandbox/context_engine.py run \
  --input data/<corpus_dir>/ \
  --work-dir /tmp/context-engine

# Skip audio (use cached transcripts from work_dir/transcripts/) — fast iteration
python3 sandbox/context_engine.py run \
  --input data/<corpus_dir>/ \
  --work-dir /tmp/context-engine \
  --skip-audio

# Override synthesis model
python3 sandbox/context_engine.py run \
  --input data/<corpus_dir>/ \
  --work-dir /tmp/context-engine \
  --model anthropic/claude-3.5-sonnet
```

Run `python3 sandbox/context_engine.py --help` to see all flags.

## Output artifacts (in work_dir/)

| File | Contents |
|------|----------|
| `items.json` | Parsed items (text + media refs) — schema depends on parser |
| `transcripts/*.json` | One per audio file — text + speaker turns + raw ASR/diarize responses |
| `text_chunks.json` | Text items + transcripts chunked for entity extraction |
| `entities.json` | Per-chunk extracted entities (people/orgs/topics/locations/dates) |
| `synthesis_context.json` | What the synthesis LLM saw (corpus summary + voice + text) |
| `intelligence_report.md` | Final structured intelligence report |

## Long-audio handling

Audio > 60s is automatically chunked:
1. `ffprobe` measures duration
2. Audio split into 60s windows with 2s overlap (ffmpeg subprocess)
3. Each chunk transcribed separately
4. Tokens + timestamps stitched with offset + overlap-dedup
5. Speaker alignment runs on the merged transcript

Verified on 7.75-minute voice file → 3 speakers, 51 turns, 7249-char transcript.

## Cost + latency (observed baseline)

For a small mixed corpus (a handful of voice files + text):
- Audio processing: ~12 min for the long file (8min audio), ~30s each for short
- Entity extraction: ~30s × N chunks (1 LLM call per chunk)
- Synthesis: ~30s single LLM call
- **Total wall clock**: ~15 min for a small demo
- **SurrealDB graph**: transcripts + entities + mention edges proportional to corpus size
- **Report length**: typically 3–6K chars

Larger corpora scale roughly linearly with audio duration; pyannote on CPU is the bottleneck for long-form voice.

## Quality notes

- Speaker labels are abstract (SPEAKER_00, SPEAKER_01) until cross-modal linking resolves them to person nodes
- Identity resolution depends on coverage of both face embeddings (from images/video keyframes) and voice embeddings (from audio tracks)
- Entity extraction has some noise — could be filtered with allow/deny lists per corpus
- Synthesis is one-shot — could be improved with iterative refinement (draft → critique → revise)

## Failure modes (silent)

The auditor role should watch for:
- Transcripts with empty `text` field (ASR failed)
- Transcripts with `speakers: []` (diarization failed)
- Entities with `people: []` across ALL chunks (LLM endpoint broken)
- Report with `## Information Gaps` listing majority of content (context not loaded)
- Embedding coverage gaps (item/transcript/face rows missing their vector columns — see audit Check 7)

## Files

- `sandbox/context_engine.py` — main orchestrator
- `sandbox/audio_client.py` — per-file audio processing
- `sandbox/entity_extract.py` — LLM-based entity extraction
- `sandbox/surreal_client.py` — knowledge graph store
- `sandbox/<format>_parser.py` — one parser per supported ingest format; `--help` lists flags + edge cases
