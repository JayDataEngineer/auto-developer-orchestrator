"""Offline tests for settings.py — multi-account discovery from env vars.

No network, no twscrape. Verifies the regex extraction logic that turns the
flat NITTER_ACCOUNT_<KEY>_<FIELD> env vars into structured TwitterAccount
objects. Catches regressions in the env var naming convention.
"""
from __future__ import annotations

import importlib
import os
from types import SimpleNamespace

import pytest


def _reload_settings_with_env(env: dict[str, str]):
    """Reimport the settings module under a mocked environment."""
    original = os.environ.copy()
    os.environ.clear()
    os.environ.update(env)
    try:
        import src.settings as mod
        # Reset lru_cache so get_settings() re-reads env.
        mod.get_settings.cache_clear()
        importlib.reload(mod)
        return mod.get_settings()
    finally:
        os.environ.clear()
        os.environ.update(original)
        # And again, to clear the cached reloaded state.
        import src.settings as mod2
        mod2.get_settings.cache_clear()
        importlib.reload(mod2)


def test_no_accounts_when_env_empty():
    s = _reload_settings_with_env({})
    assert s.accounts == []
    assert s.host == "0.0.0.0"
    assert s.port == 41730
    assert s.max_limit == 200


def test_single_account_extracted():
    s = _reload_settings_with_env(
        {
            "NITTER_ACCOUNT_ALICE_USERNAME": "alice",
            "NITTER_ACCOUNT_ALICE_EMAIL": "alice@example.com",
            "NITTER_ACCOUNT_ALICE_PASSWORD": "p1",
            "NITTER_ACCOUNT_ALICE_EMAIL_PASSWORD": "ep1",
            "NITTER_ACCOUNT_ALICE_AUTH_TOKEN": "tok1",
            "NITTER_ACCOUNT_ALICE_CT0": "ct1",
        }
    )
    assert len(s.accounts) == 1
    a = s.accounts[0]
    assert a.username == "alice"
    assert a.email == "alice@example.com"
    assert a.password == "p1"
    assert a.email_password == "ep1"
    assert a.auth_token == "tok1"
    assert a.ct0 == "ct1"


def test_multiple_accounts_round_robinned_into_list():
    s = _reload_settings_with_env(
        {
            "NITTER_ACCOUNT_USER1_USERNAME": "alice",
            "NITTER_ACCOUNT_USER1_EMAIL": "a@x.com",
            "NITTER_ACCOUNT_USER1_PASSWORD": "p1",
            "NITTER_ACCOUNT_USER1_EMAIL_PASSWORD": "ep1",
            "NITTER_ACCOUNT_USER1_AUTH_TOKEN": "t1",
            "NITTER_ACCOUNT_USER1_CT0": "c1",
            "NITTER_ACCOUNT_USER2_USERNAME": "bob",
            "NITTER_ACCOUNT_USER2_EMAIL": "b@x.com",
            "NITTER_ACCOUNT_USER2_PASSWORD": "p2",
            "NITTER_ACCOUNT_USER2_EMAIL_PASSWORD": "ep2",
            "NITTER_ACCOUNT_USER2_AUTH_TOKEN": "t2",
            "NITTER_ACCOUNT_USER2_CT0": "c2",
        }
    )
    assert len(s.accounts) == 2
    usernames = {a.username for a in s.accounts}
    assert usernames == {"alice", "bob"}


def test_account_skipped_when_auth_token_missing():
    """Incomplete account (no AUTH_TOKEN) is dropped silently — the loader
    logs it via the client at startup, not here."""
    s = _reload_settings_with_env(
        {
            "NITTER_ACCOUNT_PARTIAL_USERNAME": "pat",
            "NITTER_ACCOUNT_PARTIAL_CT0": "c",  # has ct0 but no auth_token
        }
    )
    assert s.accounts == []


def test_account_skipped_when_ct0_missing():
    s = _reload_settings_with_env(
        {
            "NITTER_ACCOUNT_PARTIAL_USERNAME": "pat",
            "NITTER_ACCOUNT_PARTIAL_AUTH_TOKEN": "tok",
        }
    )
    assert s.accounts == []


def test_username_falls_back_to_env_key_when_missing():
    """If NITTER_ACCOUNT_<KEY>_USERNAME isn't set, we infer from <KEY>
    (lowercased, stripped of leading underscores)."""
    s = _reload_settings_with_env(
        {
            # No _USERNAME field — fall back to key "CEMRESGDX" → "cemresgdx".
            "NITTER_ACCOUNT_CEMRESGDX_AUTH_TOKEN": "tok",
            "NITTER_ACCOUNT_CEMRESGDX_CT0": "ct",
        }
    )
    assert len(s.accounts) == 1
    assert s.accounts[0].username == "cemresgdx"


def test_env_prefix_does_not_collide_with_account_vars():
    """NITTER_HOST, NITTER_PORT, NITTER_DB_PATH etc. should NOT be picked up
    as account fields. The regex requires the field suffix to be exactly
    AUTH_TOKEN|CT0|USERNAME|EMAIL|PASSWORD|EMAIL_PASSWORD."""
    s = _reload_settings_with_env(
        {
            "NITTER_HOST": "1.2.3.4",
            "NITTER_PORT": "9999",
            "NITTER_DB_PATH": "/tmp/x.db",
            "NITTER_MAX_LIMIT": "50",
            "NITTER_ACCOUNT_ALICE_USERNAME": "alice",
            "NITTER_ACCOUNT_ALICE_AUTH_TOKEN": "tok",
            "NITTER_ACCOUNT_ALICE_CT0": "ct",
        }
    )
    assert s.host == "1.2.3.4"
    assert s.port == 9999
    assert s.db_path == "/tmp/x.db"
    assert s.max_limit == 50
    assert len(s.accounts) == 1
