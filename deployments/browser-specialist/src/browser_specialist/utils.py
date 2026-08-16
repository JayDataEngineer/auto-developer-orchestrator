"""Model builder — the workspace's glm-5.2 endpoint (env-driven, no secrets)."""

from __future__ import annotations

import os


def build_model():
    """ChatOpenAI against the configured OpenAI-compatible endpoint.

    Mirrors the workspace's `pux-openai` provider (base_url + env-keyed token):
    `ANTHROPIC_AUTH_TOKEN` carries the key, `BROWSER_SPECIALIST_MODEL` /
    `BROWSER_SPECIALIST_BASE_URL` allow overrides without touching code.
    """
    from langchain_openai import ChatOpenAI

    return ChatOpenAI(
        model=os.environ.get("BROWSER_SPECIALIST_MODEL", "glm-5.2"),
        base_url=os.environ.get(
            "BROWSER_SPECIALIST_BASE_URL", "https://api.z.ai/api/coding/paas/v4"
        ),
        api_key=os.environ["ANTHROPIC_AUTH_TOKEN"],
        timeout=300,
    )
