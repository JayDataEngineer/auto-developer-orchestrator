from pathlib import Path

SUBAGENT = {
    "name": 'dre-writer',
    "description": "Deep Research Engine content writer — adapts a cited brief (artifacts/brief.md) for a target channel. One agent, parameterized by the CTO's task string. Channels include substack (longform article), twitter/x (single or thread), mastodon, bluesky, linkedin.",
    "tools": ['python'],
    "skills": ['orgs/deep-research-engine/skills'],
    "system_prompt": Path(__file__).with_suffix(".md").read_text(),
}
