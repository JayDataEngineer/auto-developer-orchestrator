from pathlib import Path

SUBAGENT = {
    "name": 'twitter-drafter',
    "description": 'Twitter Agent drafting specialist — reads recent timeline context (via twitter_helpers.py), drafts a tweet or thread for a requested content slot. Authentic voice, no engagement bait. Does NOT post — the CTO posts.',
    "skills": ['orgs/twitter-agent/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
