# Summarize Videos (MiMo-V2.5 + Open Diarization)

## When to use this

**Condition:** the dataset contains video files (`.mp4`, `.mov`, `.avi`, `.webm`).

**If no videos exist:** skip this entirely. This skill is conditional — never run it on a dataset that has no video files. Query the item table first:

```sql
SELECT count() AS n FROM item WHERE type = 'video' GROUP ALL;
```

If `n = 0`, stop. Do not run the summarizer.

## What it does

For each video:
1. **Structural summary** via MiMo-V2.5 (cloud multimodal model, 1M token context, Video-MME 87.7%). The model sees the ENTIRE video (not just keyframes) and produces a structured analysis: what's happening, who's present, timeline, visible text, audio/speech, setting/context, key takeaways.
2. **Speaker diarization** via an open pipeline (ffmpeg → webrtcvad → resemblyzer → HDBSCAN). Identifies WHO speaks WHEN. Non-gated (no HuggingFace token needed — unlike pyannote).

Both outputs are paired together so the researcher can see "this person said X at timestamp Y while Z was visible on screen."

## How to run

```bash
# Inside the sandbox (resemblyzer/webrtcvad are sandbox deps):
python3 orgs/specialists/deep-research-engine/sandbox/video_summarize.py
```

Environment variables (all optional — sensible defaults):
- `VIDEO_DIR` — directory of source videos (default: dataset video_files/)
- `RUN_DIR` — artifact run directory
- `MEDIA_MCP_URL` — media-mcp endpoint for cloud_vlm (default: http://localhost:8101/mcp)
- `SURREALDB_URL` — if set, links summaries to item records in the graph
- `DIARIZE` — `1` (default) to run diarization, `0` to skip

## Output artifacts

- `entities/video_frames/video_summaries.json` — machine-readable: `{video_stem: {summary, speaker_segments, n_speakers}}`
- `entities/video_frames/video_summaries.md` — human-readable browse index

If `SURREALDB_URL` is set, each video item record gets:
- `video_summary` field (the structured analysis text)
- `speaker_turns` field (the diarization segments)

## How it works (technical)

### Video summary (cloud_vlm)
The script encodes each video as a **data URI** (base64-inline) because the cloud provider cannot fetch `localhost` URLs. The data URI is sent to media-mcp's `cloud_vlm` tool, which dispatches to MiMo-V2.5. The model's `response` field contains the structured summary text.

**Large videos (12MB+):** the base64 expansion (~33% overhead) can produce 16MB+ data URIs. These work but are slow. The script handles failures gracefully and caches successful results.

### Speaker diarization (open pipeline)
Pyannote is **gated** (needs a HuggingFace token + license acceptance). This skill uses an equivalent pipeline from open components:

1. **ffmpeg** extracts a 16kHz mono WAV from the video's audio track.
2. **webrtcvad** (Google WebRTC VAD, aggressiveness 2) finds speech segments in 30ms frames. Segments shorter than 0.5s are discarded.
3. **resemblyzer VoiceEncoder** (VoxCeleb ResNet34, 256-d L2-normalized) embeds each speech segment.
4. **HDBSCAN** (cosine metric) clusters segments by speaker identity.

Each segment gets a speaker label: `SPEAKER_00`, `SPEAKER_01`, etc. Noise segments (cluster -1) are labeled `SPEAKER_NOISE`.

**Limitations:** with very few segments (<5), HDBSCAN may assign everything to one cluster or mark all as noise. This is expected — the pipeline improves with longer videos that have more speaker turns.

## Resumability

The script is **idempotent and resumable**:
- Summaries are cached in `video_summaries.json`. Re-running skips videos that already have summaries (>20 chars).
- If a summary exists but diarization is missing, the script runs ONLY diarization on the next pass (no wasted cloud API calls).
- If a summary is `"{}"` or very short (<20 chars), it's treated as invalid and re-run.

## Identity linking

Video summaries and diarization feed into identity resolution:
- Video audio is embedded during voice clustering → video items get `speaks_in` edges from voice cluster person nodes.
- When a video has BOTH face appearances (from keyframes) AND voice appearances (from audio), the face and voice clusters are cross-linked via `same_as` edges. This is the **strongest cross-modal identity signal**.

See `INGEST_MULTIMODAL_PERSONS.md` for the full identity resolution strategy.

## Using summaries in research

When writing reports, query the graph for video context:

```sql
-- Get all video summaries with speaker info
SELECT path, video_summary, speaker_turns
FROM item WHERE type = 'video'
AND video_summary != NONE;
```

Pair video summaries with audio summaries and photo analysis for multi-modal grounding. The structured format (what/who/when/text/audio/context/takeaways) makes it easy to extract specific facts.
