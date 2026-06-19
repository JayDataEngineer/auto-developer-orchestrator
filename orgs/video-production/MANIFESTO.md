# Video Production

Full-stack educational video production org. Takes a topic, paper, or transcript and produces a finished narrated video — research, script, storyboard, Manim/ffmpeg visuals, Kokoro TTS narration, QC, archive, deliver.

**Reference skill**: https://github.com/Aaryan-Kapoor/video-production-skill (MIT, by Aaryan Kapoor). This org is a Pux port of that skill — same workspace contract, same scripts, wrapped as a containerized org with a delegated Video Producer role.

## Standards

- Treat every request as a finished deliverable, not a plan.
- Source-grounded claims; cite or fetch the original.
- Polished visuals: animated diagrams, charts, cleaned-up crops. No unreadable screenshots.
- Narration normalized via ffmpeg loudnorm.
- QC before delivery: extract preview frames, verify audio stream, check for cropped text.
- Archive every final MP4 into the job's `exports/` and `backups/`.

## Stack

- **Container**: `research-video-producer` (Python 3.11, Manim 0.19, Kokoro TTS, ffmpeg, poppler, LaTeX)
- **Skills**: skills/SKILL.md ships the Standard Workflow + helper scripts (`init_video_job.py`, `synth_kokoro.py`, `archive_video.py`, `host_video.py`)
- **Network**: shares `deep-research-engine_default` so the producer can reach `research-media-mcp` and `research-web-mcp`
