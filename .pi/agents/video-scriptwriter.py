from pathlib import Path

SUBAGENT = {
    "name": 'video-scriptwriter',
    "description": 'Video Production scriptwriter — writes the narration script + segment manifest from a brief. Produces src/segments.json + src/production_brief.md.',
    "tools": ['python'],
    "skills": ['orgs/video-production/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
