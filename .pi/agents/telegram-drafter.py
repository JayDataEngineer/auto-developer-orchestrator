from pathlib import Path

SUBAGENT = {
    "name": 'telegram-drafter',
    "description": 'Telegram Agent drafting specialist — reads recent chat context (via telegram_helpers.py), drafts a reply or proactive post for a target chat. Tone-sensitive. Does NOT send — the CTO sends.',
    "skills": ['orgs/telegram-agent/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
