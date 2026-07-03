"""Model config: mimo-v2.5 via OpenCode Zen Go (OpenAI-compatible router).

OpenCode Zen Go is an OpenAI-compatible router at https://opencode.ai/zen/go/v1
(api key: $OPENCODE_API_KEY). mimo-v2.5 is a *reasoning* model — the probe
(2026-07-03) showed it emits `reasoning_content` and can return `content: null`
with `finish_reason: length` if max_tokens is too small. We set a generous
max_tokens so reasoning completes and content/tool_calls are produced.

If mimo's reasoning breaks the agent loop (null content, no tool_calls), set
`PUX_MODEL=glm-5.2` — clean, non-reasoning, on the same endpoint — and rerun.
The model is intentionally swappable so the architecture spike isn't gated on
a single model's quirks.
"""
from __future__ import annotations

import os

from deepagents import HarnessProfile, register_harness_profile
from langchain_openai import ChatOpenAI

BASE_URL = os.environ.get("OPENCODE_BASE_URL", "https://opencode.ai/zen/go/v1")
DEFAULT_MODEL = os.environ.get("PUX_MODEL", "mimo-v2.5")

# deepagents' default middleware adds an in-memory virtual filesystem
# (ls/read_file/write_file/edit_file/glob/grep) backed by an empty
# StateBackend, plus `execute` when a sandbox backend is present. We pass
# neither — the real filesystem is the pux-sandbox container, reached via
# the `pux_sandbox_*` tools. Leaving the native fs tools visible lets a weak
# model grab `read_file` (returns empty) instead of `pux_sandbox_file_read`
# — the exact wrong-tool failure mode this pivot exists to kill. Excluding
# them makes the sandbox the only file/shell surface. Registered provider-
# wide under "openai" so it applies to any ChatOpenAI model (mimo, glm, …)
# via the provider-prefix profile fallback, surviving PUX_MODEL swaps.
_NATIVE_FS_TOOLS = frozenset(
    {"ls", "read_file", "write_file", "edit_file", "glob", "grep", "execute"}
)


def register_pux_profile() -> None:
    """Hide deepagents' native in-memory fs/shell tools so only the real
    sandbox surface (`pux_sandbox_*`) is visible to the model.

    Verified 2026-07-03: a ChatOpenAI(model='mimo-v2.5') pre-built model
    resolves this profile (key path `openai` -> provider-prefix fallback in
    `_harness_profile_for_model`), and a bind_tools capture confirmed
    `NATIVE FS LEAKED: NONE` — the model-visible set is exactly the 19
    `pux_sandbox_*` tools + `task` + `write_todos`.
    """
    register_harness_profile("openai", HarnessProfile(excluded_tools=_NATIVE_FS_TOOLS))


def get_model(model: str | None = None) -> ChatOpenAI:
    return ChatOpenAI(
        model=model or DEFAULT_MODEL,
        base_url=BASE_URL,
        api_key=os.environ["OPENCODE_API_KEY"],
        timeout=180,
        # OpenCode Zen Go is a free router with a tight per-account rate limit
        # (429 "provider_rate_limit_exceeded"). Let the OpenAI client ride
        # transient limits with its built-in exponential backoff (~30-60s across
        # 6 retries) rather than dying on the first 429. Standard client
        # resilience, not a behavior fallback.
        max_retries=6,
        max_tokens=int(os.environ.get("PUX_MAX_TOKENS", "8192")),
        temperature=float(os.environ.get("PUX_TEMPERATURE", "0.2")),
    )
