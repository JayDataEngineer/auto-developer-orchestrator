# Context Engine — Autonomous Ingestion → Intelligence Report

Standalone orchestrator that takes raw communications data and produces a
structured intelligence report with zero human-in-the-loop decisions.

## When to use

- Telegram chat export → intelligence analysis
- Bulk audio (interviews, voice messages) → searchable knowledge graph
- Mixed media corpus → entity-rich narrative report

## Pipeline

```
[1] Survey        →  count files by type (audio/image/text/other)
[2] Parse         →  telegram_parser.py → items.json (text + media refs)
[3] Audio         →  audio_client.py process per file → transcripts/*.json
                     (Parakeet ASR + Pyannote diarization + speaker alignment)
[4] Entities      →  entity_extract.py batch → entities.json
                     (LLM extracts people/organizations/topics/locations/dates
                      per chunk)
[5] Knowledge Graph → surreal_client.py → SurrealDB
                     (transcript records + entities + mention edges)
[6] Synthesis     →  LLM reads whole corpus → intelligence_report.md
                     (structured markdown: actors, themes, timeline, warnings)
```

## Usage

```bash
cd ~/Documents/programs/deep-research-engine

# Configure LLM endpoint (OpenRouter auto-detected from .env)
export $(grep -E "^(OPENROUTER_API_KEY|OPENROUTER_BASE_URL)=" .env | xargs)

# Full autonomous run on a Telegram export
python3 sandbox/context_engine.py run \
  --input data/ChatExport_2026-03-13/ \
  --work-dir /tmp/context-engine

# Skip audio (use cached transcripts from work_dir/transcripts/) — fast iteration
python3 sandbox/context_engine.py run \
  --input data/ChatExport_2026-03-13/ \
  --work-dir /tmp/context-engine \
  --skip-audio

# Override synthesis model
python3 sandbox/context_engine.py run \
  --input data/ChatExport_2026-03-13/ \
  --work-dir /tmp/context-engine \
  --model anthropic/claude-3.5-sonnet
```

## Output artifacts (in work_dir/)

| File | Contents |
|------|----------|
| `items.json` | Parsed Telegram items (text + media refs) |
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

## Cost + latency (observed 2026-06-18)

For the Telegram demo (4 voice files + 13 text messages):
- Audio processing: ~12 min for the long file (8min audio), ~30s each for short
- Entity extraction: ~30s × N chunks (1 LLM call per chunk)
- Synthesis: ~30s single LLM call
- **Total wall clock**: ~15 min for 4-file demo
- **SurrealDB graph**: 4 transcripts + 99 entities + 34 mention edges
- **Report length**: 5223 chars

For the full 34-file Telegram export, projected ~2-3 hours (bottleneck: pyannote CPU on long audio).

## Quality notes

- Speaker labels are abstract (SPEAKER_00, SPEAKER_01) — no identity resolution across files yet
- Identity resolution would require voice fingerprints or face matching against known senders
- Entity extraction has some noise (e.g., "Telegram" classified as organization) — could be filtered with allow/deny lists
- Synthesis is one-shot — could be improved with iterative refinement (draft → critique → revise)

## Failure modes (silent)

The auditor role should watch for:
- Transcripts with empty `text` field (ASR failed)
- Transcripts with `speakers: []` (diarization failed)
- Entities with `people: []` across ALL chunks (LLM endpoint broken)
- Report with `## Information Gaps` listing majority of content (context not loaded)

## Files

- `sandbox/context_engine.py` — main orchestrator
- `sandbox/audio_client.py` — per-file audio processing
- `sandbox/entity_extract.py` — LLM-based entity extraction
- `sandbox/surreal_client.py` — knowledge graph store
- `sandbox/telegram_parser.py` — Telegram export parser
