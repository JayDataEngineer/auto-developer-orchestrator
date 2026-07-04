from pathlib import Path

SUBAGENT = {
    "name": 'dev-bot-tester',
    "description": "Test engineering specialist for the Dev-Bot engineering org — writes tests, runs them, reports pass/fail with evidence. Proves behavior, doesn't assert it.",
    "tools": ['python'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
