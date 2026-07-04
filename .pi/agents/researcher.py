from pathlib import Path

SUBAGENT = {
    "name": 'researcher',
    "description": 'Read-only codebase investigator — answers specific questions with cited evidence from files',
    "tools": ['python'],
    "skills": ['.pi/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
