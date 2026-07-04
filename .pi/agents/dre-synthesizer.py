from pathlib import Path

SUBAGENT = {
    "name": 'dre-synthesizer',
    "description": 'Deep Research Engine synthesizer — merges gathered findings (web research, PDF ingest, DB queries) into a single cited brief at artifacts/brief.md. Resolves conflicts, flags uncertainty, every claim traceable.',
    "tools": ['python'],
    "skills": ['orgs/deep-research-engine/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
