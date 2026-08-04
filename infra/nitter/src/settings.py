"""Settings for the nitter-mcp service.

Reads from environment with pydantic-settings. Account pool is auto-populated
from any ``NITTER_ACCOUNT_*_AUTH_TOKEN`` + ``_CT0`` pairs present in the env
(typically loaded from the gitignored ``infra/nitter/.env`` file via docker
compose ``env_file:``). At least one account is required; ``AccountsPool``
will round-robin requests across all configured accounts to spread Twitter's
per-account rate limits.

Design notes
------------
- READ-ONLY service. Writes (post/like/RT/follow) belong to the browser MCP
  at the twitter-agent org, not here. The tools below intentionally expose
  no mutating surface.
- ``stateless_http=True`` (set in server.py) means each FastMCP request opens
  a fresh session — ``TwitterReadClient`` keeps the twscrape pool warm
  in-process across requests via the lifespan context.
"""
from __future__ import annotations

import os
from functools import lru_cache
from typing import List

from pydantic import BaseModel, Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

# Known field suffixes on NITTER_ACCOUNT_<KEY>_<FIELD> env vars.
# Order matters: when extracting, we try the LONGEST field name first so
# ``EMAIL_PASSWORD`` wins over ``PASSWORD`` (which would otherwise leave
# ``EMAIL_`` baked into the key). A regex alternation can't express this
# priority because the greedy ``[A-Z0-9_]+`` key matches the longest key
# first — see test_single_account_extracted for the regression.
_KNOWN_FIELDS = ("EMAIL_PASSWORD", "AUTH_TOKEN", "USERNAME", "CT0", "EMAIL", "PASSWORD")
_PREFIX = "NITTER_ACCOUNT_"


def _parse_account_env_var(name: str) -> tuple[str, str] | None:
    """Split ``NITTER_ACCOUNT_<KEY>_<FIELD>`` into (key, field_lower).

    Returns None if the name doesn't match. Tries the longest field suffix
    first so EMAIL_PASSWORD is recognized as one field, not ``EMAIL`` + key
    residue."""
    if not name.startswith(_PREFIX):
        return None
    rest = name[len(_PREFIX):]
    for field in _KNOWN_FIELDS:
        suffix = "_" + field
        if rest.endswith(suffix):
            key = rest[:-len(suffix)]
            if key and key.replace("_", "").isalnum():
                return key, field.lower()
    return None


class TwitterAccount(BaseModel):
    """One Twitter login. Cookies are the primary auth; password/email are
    fallback for twscrape's full re-login flow if the cookies die."""

    username: str
    auth_token: str
    ct0: str
    email: str | None = None
    password: str | None = None
    email_password: str | None = None


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="NITTER_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        case_sensitive=False,
    )

    # Server bind
    host: str = "0.0.0.0"
    port: int = 41730

    # twscrape SQLite DB location. Mounted as a docker volume for persistence
    # — keeps the login cache across container restarts so we don't re-auth.
    db_path: str = "/data/accounts.db"

    # Per-request HTTP timeout for upstream Twitter GraphQL calls.
    twitter_timeout_s: float = 30.0

    # Hard ceiling on any tool's ``limit`` argument. Twitter's GraphQL pages
    # are ~20 items; twscrape walks the cursor for us, but unbounded walks
    # are a fast way to burn the account. Default 200 = 10 pages.
    max_limit: int = 200

    # When True, the server starts even if zero accounts are configured
    # (tools will return a clear error per call). Useful for first-time
    # bring-up diagnostics. Production should be False.
    allow_no_accounts: bool = False

    # Populated lazily by _load_accounts — not an env var itself.
    accounts: List[TwitterAccount] = Field(default_factory=list, exclude=True)

    @model_validator(mode="after")
    def _load_accounts(self) -> "Settings":
        """Discover accounts from env vars shaped like:

            NITTER_ACCOUNT_<USERNAME>_AUTH_TOKEN=...
            NITTER_ACCOUNT_<USERNAME>_CT0=...
            NITTER_ACCOUNT_<USERNAME>_USERNAME=<username>
            NITTER_ACCOUNT_<USERNAME>_EMAIL=<email>            (optional)
            NITTER_ACCOUNT_<USERNAME>_PASSWORD=<password>      (optional)
            NITTER_ACCOUNT_<USERNAME>_EMAIL_PASSWORD=<pwd>     (optional)

        We intentionally do NOT read these via pydantic-settings field
        binding — the account count is dynamic and pydantic can't model
        ``NITTER_ACCOUNT_*`` cleanly. Scan + group manually.
        """
        bucket: dict[str, dict[str, str]] = {}
        for env_name, env_val in os.environ.items():
            parsed = _parse_account_env_var(env_name)
            if not parsed or not env_val:
                continue
            key, field = parsed
            bucket.setdefault(key, {})[field] = env_val

        for key, fields in bucket.items():
            auth = fields.get("auth_token")
            ct0 = fields.get("ct0")
            if not auth or not ct0:
                continue  # incomplete — skip loudly via logs in client
            username = fields.get("username") or key.lower().lstrip("_")
            self.accounts.append(
                TwitterAccount(
                    username=username,
                    auth_token=auth,
                    ct0=ct0,
                    email=fields.get("email"),
                    password=fields.get("password"),
                    email_password=fields.get("email_password"),
                )
            )
        return self


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()
