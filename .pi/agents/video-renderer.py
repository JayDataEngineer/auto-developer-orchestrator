from pathlib import Path

SUBAGENT = {
    "name": 'video-renderer',
    "description": 'Video Production renderer — drives Manim + ffmpeg + Kokoro end-to-end. Produces exports/final.mp4 with synced narration, archives to backups/.',
    "tools": ['python'],
    "skills": ['orgs/video-production/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
