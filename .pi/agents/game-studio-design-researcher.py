from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-design-researcher',
    "description": 'Tech Noir Design Researcher — researches game design topics (survival horror mechanics, 2.5D Godot techniques, procedural gen, AI asset pipelines, narrative design, audio) and produces under-400-word actionable reports. Read-only.',
    "tools": ['python', 'browser_navigate', 'browser_screenshot', 'describe_image'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
