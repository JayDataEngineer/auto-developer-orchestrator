#!/usr/bin/env bash
# ACP stdio server wrapper for Zed editor.
# Sources .env (API keys, etc.) then launches `pux acp`.
# Zed spawns this as a child process — stdin/stdout are the ACP wire.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"

if [[ -f "$REPO/.env" ]]; then
  set -a
  . "$REPO/.env"
  set +a
fi

exec "$REPO/bin/pux" acp "$@"
