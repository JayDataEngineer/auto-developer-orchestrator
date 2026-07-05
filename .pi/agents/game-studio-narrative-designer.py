from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-narrative-designer',
    "description": 'Game Studio Narrative Designer — the creative voice. Writes dialogue, builds lore, shapes characters, brainstorms story. Two modes: brainstorm (loose ideas) and write (drafts to departments/narrative/). Read-only on code + pipeline.',
    "tools": ['python', 'browser_navigate', 'browser_screenshot', 'describe_image'],
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
