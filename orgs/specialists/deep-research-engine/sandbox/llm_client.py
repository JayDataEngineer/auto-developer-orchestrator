#!/usr/bin/env python3
"""Universal LLM client for DRE sandbox scripts.

Replaces the per-script duplicated call_llm + raw HTTP pattern.
Resolves the model from models.yaml -- the SAME single source of truth the
harness uses (policy.py::_resolve_llm_endpoint).

Resolution chain:
  1. models.yaml -> tier (default) -> role (worker) -> model id
  2. models.yaml -> model id -> provider profile (base_url, api_key_env)
  3. .env -> api_key_env -> actual key value
  4. Builds api_url = f"{base_url}/chat/completions"
"""

from __future__ import annotations

import json
import os
from pathlib import Path

_WORKSPACE_ROOT = Path("/sandbox/workspace")
_MODELS_YAML = _WORKSPACE_ROOT / "pux-harness" / "pux_harness" / "agent" / "models.yaml"


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


def _resolve_endpoint(role="worker"):
    """Resolve a model role to (api_url, model_id, api_key) via models.yaml."""
    import yaml

    if not _MODELS_YAML.exists():
        raise RuntimeError(
            f"models.yaml not found at {_MODELS_YAML}. "
            "Cannot resolve LLM endpoint."
        )

    with _MODELS_YAML.open() as f:
        spec = yaml.safe_load(f)

    tier_name = os.environ.get("PUX_TIER", spec.get("default_tier", "default"))
    tier = spec.get("tiers", {}).get(tier_name, {})
    role_key = f"{role}_model"
    model_id = tier.get(role_key)
    if not model_id:
        raise RuntimeError(
            f"Role {role!r} not found in tier {tier_name!r}."
        )

    model_entry = spec.get("models", {}).get(model_id, {})
    provider_name = model_entry.get("provider", spec.get("default_provider", ""))
    providers = spec.get("providers", {})
    provider = providers.get(provider_name, {})

    base_url = provider.get("base_url", "")
    api_key_env = provider.get("api_key_env", "")
    kind = provider.get("kind", "openai")

    if kind != "openai":
        raise RuntimeError(
            f"Model {model_id!r} uses provider kind {kind!r}. "
            "Sandbox scripts require OpenAI-compatible endpoint."
        )

    api_url = f"{base_url}/chat/completions"
    env_file = _load_env_file()
    api_key = os.environ.get(api_key_env) or env_file.get(api_key_env, "")

    return api_url, model_id, api_key


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

    Resolves endpoint from models.yaml (single source of truth).
    Handles reasoning models by disabling thinking by default.
    Falls back to reasoning_content, then retries with doubled budget.
    """
    api_url, resolved_model, api_key = _resolve_endpoint(role)
    model_id = model or resolved_model

    headers = {
        "Content-Type": "application/json",
        "User-Agent": "pux-harness-sandbox/1.0",
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
