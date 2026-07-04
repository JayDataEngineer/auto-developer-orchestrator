from pathlib import Path

SUBAGENT = {
    "name": 'dre-auditor',
    "description": 'Deep Research Engine QA specialist — verifies multimodal ingest quality (embedding coverage, transcript completeness, sender cleanliness, topic discovery, cross-modal linking). Read-only; returns gap report. Does NOT re-ingest.',
    "tools": ['python'],
    "skills": ['orgs/deep-research-engine/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
