from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-qa-tester',
    "description": 'Game Studio QA Tester — runs godot_test.py evaluate harness, analyzes screenshots via MEDIA_QA, produces vibe.json with iterate/yield/abort recommendation. Read-only on game code + art.',
    "tools": ['python', 'describe_image'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
