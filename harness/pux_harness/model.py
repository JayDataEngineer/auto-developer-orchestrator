"""Model config: mimo-v2.5 via OpenCode Zen Go (OpenAI-compatible router).

OpenCode Zen Go is an OpenAI-compatible router at https://opencode.ai/zen/go/v1
(api key: $OPENCODE_API_KEY). mimo-v2.5 is a *reasoning* model — emits
`reasoning_content` and can return `content: null` with `finish_reason: length`
if max_tokens is too small, so we set a generous max_tokens.

If mimo's reasoning breaks the loop (null content, no tool_calls), set
`PUX_MODEL=glm-5.2` — clean, non-reasoning, same endpoint — and rerun. The model
is swappable so the architecture isn't gated on one model's quirks.

Phase 3: native fs/shell tools (ls/read_file/write_file/edit_file/glob/grep/
execute) are NOT excluded — they are the visible file/shell surface, backed by
`PuxSandboxBackend` over the Go sandbox. (Phase 1 excluded them because the
default StateBackend was empty — read_file returned nothing. Supplying a real
backend fixes that, so no exclusion is wanted.)
"""
from __future__ import annotations

import os

from langchain_openai import ChatOpenAI

BASE_URL = os.environ.get("OPENCODE_BASE_URL", "https://opencode.ai/zen/go/v1")
DEFAULT_MODEL = os.environ.get("PUX_MODEL", "mimo-v2.5")


def get_model(model: str | None = None) -> ChatOpenAI:
    return ChatOpenAI(
        model=model or DEFAULT_MODEL,
        base_url=BASE_URL,
        api_key=os.environ["OPENCODE_API_KEY"],
        timeout=180,
        # OpenCode Zen Go is a free router with a tight per-account rate limit
        # (429 "provider_rate_limit_exceeded"). Let the OpenAI client ride
        # transient limits with built-in exponential backoff (~30-60s across
        # 6 retries) rather than dying on the first 429. Standard client
        # resilience, not a behavior fallback.
        max_retries=6,
        max_tokens=int(os.environ.get("PUX_MAX_TOKENS", "8192")),
        temperature=float(os.environ.get("PUX_TEMPERATURE", "0.2")),
    )
