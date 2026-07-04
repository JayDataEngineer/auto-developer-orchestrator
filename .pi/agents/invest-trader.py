from pathlib import Path

SUBAGENT = {
    "name": 'invest-trader',
    "description": 'Investment Division execution specialist — Alpaca paper trading + prediction journaling. Reads data/signals.json, executes approved trades, journals predictions BEFORE fills.',
    "tools": ['python'],
    "skills": ['orgs/invest/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
