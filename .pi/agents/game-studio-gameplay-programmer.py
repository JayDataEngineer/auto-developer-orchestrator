from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-gameplay-programmer',
    "description": 'Tech Noir Gameplay Programmer — Godot 4.6 scenes, scripts, shaders, GUT tests. Drives the Godot editor via godot_client.py (HTTP MCP bridge). Owns departments/engineering/game/.',
    "tools": ['python'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
