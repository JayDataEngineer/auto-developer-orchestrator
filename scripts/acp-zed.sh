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

# Project root = where ``orgs/`` lives (the repo root), NOT the uv ``--directory``
# (pux-harness/). ``kit._paths.project_root`` defaults to the CWD, so a launch from
# pux-harness/ would discover zero orgs — export the override, mirroring ``bin/pux``
# and ``start_pux_aegra.sh``.
export PUX_PROJECT_ROOT="$REPO"

exec uv run --directory "$REPO/pux-harness" python -m pux_harness acp "$@"
