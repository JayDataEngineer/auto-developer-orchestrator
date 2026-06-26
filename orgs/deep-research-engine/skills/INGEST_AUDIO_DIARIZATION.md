# INGEST_AUDIO_DIARIZATION

Transcribe voice messages and assign speaker labels via the local
media-analysis-mcp container. Each voice file produces a `transcript`
node + N `speaker_turn` nodes in SurrealDB.

## When to use

You have audio files (`.ogg`, `.mp3`, `.wav`, `.m4a`, `.opus`, `.webm`)
from a structured export, scraped media, or interview recordings. You want
to answer "what was said, by whom, when?"

Do NOT run this skill until you have:
- Audio files on disk (paths accessible to the sandbox).
- `./bootstrap.sh` has run (brings up `research-media-mcp` on `:8102`).
- `MEDIA_PYANNOTE_TOKEN` set in `.env` (HF token with pyannote licenses accepted).

## Identity resolution — embed_voice is now preferred

Pyannote gives **turn-taking** (who speaks when within this clip). It does NOT
give **cross-clip identity** (is "speaker_0" in clip A the same person as
"speaker_0" in clip B?). To cluster speakers across clips, use `embed_voice`
(WeSpeaker CAM++ 256-d embedding) and `cluster_embeddings` (HDBSCAN).

The full pipeline is now:
1. `transcribe_audio` → text per clip
2. `voice_activity(return_embeddings=true)` → speech segments + WeSpeaker embedding per segment
3. `cluster_embeddings` over all voice embeddings → cross-clip speaker identities
4. (Optional) `diarize_audio` for within-clip turn-taking if you need finer granularity than VAD segments

`diarize_audio` is still useful for **multi-speaker clips** (interviews, meetings) where VAD alone won't separate speakers. For single-speaker voice messages (the common case), `embed_voice` per clip is enough.

## Tool

`sandbox/audio_client.py` — wraps the local media MCP HTTP API. Four subcommands:

| Subcommand | Purpose |
|------------|---------|
| `process --audio P --output out.json` | Full pipeline: transcribe + diarize + align |
| `transcribe --audio P` | Just Parakeet transcription (no speaker labels) |
| `diarize --audio P` | Just Pyannote diarization (no transcript text) |
| `batch --input dir/ --output dir/` | Process every audio file in a directory |

The script reads `MEDIA_MCP_URL` from env (default `http://localhost:8102`).
If the audio is on a host path that the MCP container can't see, the script
starts a throwaway HTTP server bound to `0.0.0.0` and exposes the file via
the docker bridge IP `172.17.0.1`. To override (e.g., use Tailscale), set
`AUDIO_HTTP_PUBLIC=100.x.x.x`.

## Pipeline

### Step 1 — Process one audio file (transcribe + diarize + align)

```bash
export MEDIA_MCP_URL=http://localhost:8102

python3 sandbox/audio_client.py process \
    --audio data/<export_dir>/<audio_subdir>/audio_1@13-03-2026_08-59-37.ogg \
    --output /tmp/turns.json \
    --wait-for-mcp       # blocks up to 120s for MCP to be ready on first call
```

**Output schema** (`/tmp/turns.json`):

```json
{
  "audio": "/abs/path/to/<audio_file>.ogg",
  "duration_sec": 12.34,
  "transcript": "Hey it's me, just calling to ...",
  "speakers": ["speaker_0", "speaker_1"],
  "turns": [
    {"speaker": "speaker_0", "start": 0.0, "end": 4.21, "text": "Hey it's me,"},
    {"speaker": "speaker_1", "start": 4.40, "end": 8.10, "text": "just calling to ..."}
  ],
  "_raw": {"transcribe": {...}, "diarize": {...}}
}
```

**Alignment algorithm**: Parakeet emits word-level timestamps; we group words
into chunks separated by >0.5s silences. Each chunk is assigned the speaker
whose Pyannote window contains the chunk's midpoint. If no window overlaps
(gap in diarization), the nearest speaker by start time is used.

### Step 2 — Batch process a directory

```bash
python3 sandbox/audio_client.py batch \
    --input data/<export_dir>/<audio_subdir>/ \
    --output /tmp/voice_turns/ \
    --wait-for-mcp
```

Writes `<filename>.json` per audio file + `_summary.json` with counts.
Failed files appear in `_summary.json` with an `error` field.

### Step 3 — Write to SurrealDB

Each audio file → 1 `media` node (if not already present from a prior
INGEST_STRUCTURED_EXPORT run) + 1 `transcript` node + N `speaker_turn` nodes:

```python
import json
from surreal_client import SurrealClient

rec = json.loads(open("/tmp/turns.json").read())
client = SurrealClient()

# Media node (idempotent — uses SurrealDB record ID = path hash)
media_id = client.insert("media", {
    "path": rec["audio"],
    "type": "audio",
    "duration_sec": rec["duration_sec"],
})[0]["id"]

# Transcript node with full text + speakers list
transcript_id = client.insert("transcript", {
    "media": media_id,
    "text": rec["transcript"],
    "speakers": rec["speakers"],
    "language": "en",  # Parakeet auto-detects; override if known
})[0]["id"]

# Speaker turns — one record per turn, linked to transcript
for turn in rec["turns"]:
    client.insert("speaker_turn", {
        "transcript": transcript_id,
        "speaker": turn["speaker"],
        "start_sec": turn["start"],
        "end_sec": turn["end"],
        "text": turn["text"],
    })

# Edge: media → transcript (transcribed_by)
client.relate(media_id, "transcribed_by", transcript_id)
```

For embedding-based search over transcripts, embed `rec["transcript"]`
separately (deferred to Feature 6 — entity extraction provides the
LLM-based summary that gets embedded).

## Auditor contract (load-bearing)

**A voice message MUST have:**
1. **Non-empty `transcript`** — empty string means Parakeet failed silently.
   Either the audio is silent, the file is corrupt, or the MCP is broken.
2. **At least one `turn`** with a `speaker` label — zero turns means
   diarization failed and no fallback was applied.

If either fails: re-run with `--wait-for-mcp` to rule out cold-start. If
still failing on a known-good file, the auditor escalates to the human.

**Name pollution guard:** speaker labels from Pyannote are pseudonymous
(`speaker_0`, `speaker_1`, ...). They are NOT real names. The identity
resolution skill (Feature 8) maps them to real persons via voice embeddings
or contextual clues.

## Smoke test

After `./bootstrap.sh`, verify the audio pipeline works end-to-end with a
single known voice file:

```bash
export MEDIA_MCP_URL=http://localhost:8102

# 1. Health check
curl -sf http://localhost:8102/health || echo "MCP not up"

# 2. Process one file (use any .ogg/.mp3 with speech)
python3 sandbox/audio_client.py process \
    --audio data/<export_dir>/<audio_subdir>/<some_file>.ogg \
    --output /tmp/smoke.json \
    --wait-for-mcp

# 3. Verify the auditor contract
python3 -c "
import json
r = json.load(open('/tmp/smoke.json'))
assert r['transcript'].strip(), 'EMPTY TRANSCRIPT — silent failure'
assert len(r['turns']) >= 1, 'NO TURNS — diarization failed'
print(f\"OK: {len(r['speakers'])} speakers, {len(r['turns'])} turns, {len(r['transcript'])} chars\")
"
```

Expected output: `OK: 1 speakers, 1 turns, 47 chars` (numbers vary by file).

## Troubleshooting

- **`MCP not healthy at http://localhost:8102/health within 120s`** — the
  container is still downloading pyannote models on first run (~1-2 GB).
  Check `docker logs research-media-mcp` — wait for "Application startup
  complete" before retrying. Subsequent runs use the cached models.

- **`HTTP 400 from /diarize_audio`** — pyannote license not accepted for
  the HF token. Visit:
  - https://huggingface.co/pyannote/speaker-diarization-3.1
  - https://huggingface.co/pyannote/segmentation-3.0
  Accept the license on each page, then restart the container:
  `docker compose restart media-mcp`.

- **Empty transcript but audio is fine** — Parakeet occasionally returns
  `{}` on very short clips (<0.5s). Check `rec["_raw"]["transcribe"]`
  for the raw response. Filter clips <1s out of the corpus before processing.

- **Only 1 speaker but audio clearly has 2** — Pyannote under-segments
  when speakers sound similar. Pass `--num-speakers 2` to force it. The
  align step still works regardless of speaker count.

- **Audio file inside a docker volume, sandbox can't reach it** — the
  script starts a one-shot HTTP server bound to `0.0.0.0` and serves via
  the docker bridge IP `172.17.0.1`. If that IP isn't reachable from the
  MCP container (different network), set `AUDIO_HTTP_PUBLIC=<tailscale-ip>`
  before running.
