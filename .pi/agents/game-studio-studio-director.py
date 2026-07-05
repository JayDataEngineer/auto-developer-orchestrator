from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-studio-director',
    "description": 'Game Studio Director — owns the autonomous build/QA/iterate loop. Delegates parallel work to specialists (technical-artist, gameplay-programmer, narrative-designer, design-researcher, qa-tester), collects results, runs QA, decides iterate vs yield vs abort. Logs every cycle to SurrealDB. Pure orchestration — never executes directly.',
    "tools": ['python'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
