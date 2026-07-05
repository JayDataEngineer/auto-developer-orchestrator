from pathlib import Path

SUBAGENT = {
    "name": 'game-studio-docs-writer',
    "description": 'Game Studio Documentation Writer — MDX author for the docs-site (Next.js 15 + MDX + Tailwind 4). Reads source via file_*, follows DOCS_AUTHORING skill for design tokens (zero border radius, Space Grotesk/Inter/Fira Code), updates Search.tsx docsIndex for new pages.',
    "skills": ['orgs/game-studio/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
