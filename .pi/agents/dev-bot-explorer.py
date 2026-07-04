from pathlib import Path

SUBAGENT = {
    "name": 'dev-bot-explorer',
    "description": 'Read-only codebase investigator for the Dev-Bot engineering org — maps unfamiliar territory, traces call chains, reports findings with cited evidence. No writes.',
    "tools": ['python'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
