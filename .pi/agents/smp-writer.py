from pathlib import Path

SUBAGENT = {
    "name": 'smp-writer',
    "description": 'Social Media Pipeline content writer — adapts a brief for a target platform (twitter | telegram | discord), reads data/research.json if present, writes data/options.json with 3-8 distinct, platform-native options.',
    "skills": ['orgs/social-media-pipeline/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
