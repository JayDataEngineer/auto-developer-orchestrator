#!/usr/bin/env python3
"""Universal LLM client for the deep-research skill scripts.

Replaces the per-script duplicated call_llm + raw HTTP pattern. Resolves the
model from dcode's own user config (``~/.deepagents/config.toml``) — the same
single source of truth the dcode CLI uses:

Resolution chain:
  1. $LLM_MODEL env var ("provider:model") — explicit override
  2. config.toml [models] default
  3. provider entry -> base_url + api_key_env; key from the environment or
     the workspace root's .env
  4. api_url = f"{base_url}/chat/completions"
"""

from __future__ import annotations

import json
import os
from pathlib import Path


def _workspace_root():
    """Layout-robust workspace root: env override, else nearest .git ancestor,
    else cwd (works in-repo under plugins/, in dcode's plugin cache, anywhere)."""
    for var in ("WORKSPACE_ROOT", "CLAUDE_PROJECT_DIR"):
        v = os.environ.get(var)
        if v:
            return Path(v).expanduser()
    for anc in Path(__file__).resolve().parents:
        if (anc / ".git").exists():
            return anc
    return Path.cwd()


_WORKSPACE_ROOT = _workspace_root()
_CONFIG_TOML = Path.home() / ".deepagents" / "config.toml"


def _load_env_file():
    """Load .env from workspace root into a dict."""
    env = {}
    env_path = _WORKSPACE_ROOT / ".env"
    if not env_path.exists():
        return env
    with env_path.open() as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" in line:
                k, v = line.split("=", 1)
                env[k.strip()] = v.strip().strip('"').strip("'")
    return env


def _resolve_endpoint():
    """Resolve (api_url, model_id, api_key) from ~/.deepagents/config.toml."""
    import tomllib

    if not _CONFIG_TOML.exists():
        raise RuntimeError(
            f"{_CONFIG_TOML} not found — configure a model provider first "
            "(this is dcode's own config)."
        )

    with _CONFIG_TOML.open("rb") as f:
        spec = tomllib.load(f)

    model_id = os.environ.get("LLM_MODEL") or spec.get("models", {}).get("default", "")
    if not model_id:
        raise RuntimeError("no default model in config.toml [models] and no $LLM_MODEL")

    provider_name, _, bare_model = model_id.partition(":")
    if not bare_model:
        # bare model id: use the sole provider if there is exactly one
        providers = spec.get("models", {}).get("providers", {})
        if len(providers) == 1:
            provider_name, _ = next(iter(providers.items()))
            bare_model = model_id
        else:
            raise RuntimeError(
                f"model {model_id!r} has no provider prefix and config.toml "
                f"defines {len(providers)} providers"
            )

    provider = spec.get("models", {}).get("providers", {}).get(provider_name, {})
    base_url = provider.get("base_url", "")
    api_key_env = provider.get("api_key_env", "")
    if not base_url:
        raise RuntimeError(f"provider {provider_name!r} has no base_url")

    api_url = f"{base_url}/chat/completions"
    api_key = os.environ.get(api_key_env) or _load_env_file().get(api_key_env, "")
    return api_url, bare_model, api_key


def _http_post(url, headers, payload, timeout=180):
    """HTTP POST using urllib (imported lazily to avoid routing triggers)."""
    import urllib.request
    data = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data, headers=headers)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read())


def call_llm(prompt, *, model=None, temperature=0.1, max_tokens=10000,
             disable_thinking=True, role="worker"):
    """Send a prompt to the LLM and return the response text.

    Endpoint comes from ~/.deepagents/config.toml (dcode's config); $LLM_MODEL
    overrides the model. ``role`` is accepted for call compatibility and
    ignored — there is one default model per provider now.
    Handles reasoning models by disabling thinking by default.
    Falls back to reasoning_content, then retries with doubled budget.
    """
    api_url, resolved_model, api_key = _resolve_endpoint()
    model_id = model or resolved_model

    headers = {
        "Content-Type": "application/json",
        "User-Agent": "deep-research-scripts/1.0",
    }
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    def _do_request(budget):
        payload = {
            "messages": [{"role": "user", "content": prompt}],
            "model": model_id,
            "temperature": temperature,
            "max_tokens": budget,
        }
        if disable_thinking:
            payload["thinking"] = {"type": "disabled"}
        return _http_post(api_url, headers, payload)

    try:
        result = _do_request(max_tokens)
        msg = result["choices"][0]["message"]
        content = msg.get("content")
        if not content:
            content = msg.get("reasoning_content") or ""
        if not content:
            result = _do_request(max_tokens * 2)
            msg = result["choices"][0]["message"]
            content = msg.get("content") or msg.get("reasoning_content") or ""
        if not content:
            finish = result["choices"][0].get("finish_reason")
            raise RuntimeError(
                f"LLM returned empty content (finish_reason={finish})."
            )
        return content
    except RuntimeError:
        raise
    except Exception as e:
        raise RuntimeError(
            f"LLM API call failed ({api_url}, model={model_id}): {e}"
        ) from e
