from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-creative',
    "description": 'Game Studio creative director — translates an operator brief into a YAML asset manifest + shot list. Read-only on workspace. Produces art/manifest.yaml.',
    "tools": ['python'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
