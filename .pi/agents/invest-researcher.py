from pathlib import Path

SUBAGENT = {
    "name": 'invest-researcher',
    "description": 'Investment Division research specialist — multi-signal fusion + regime detection + news/filings/on-chain overlay. Produces data/signals.json + research report.',
    "tools": ['python'],
    "skills": ['orgs/invest/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
