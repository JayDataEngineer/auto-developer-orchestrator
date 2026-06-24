#!/usr/bin/env bash
# twitter-agent bootstrap — host-side, idempotent.
#
# Sets up the Python venv used to read cookies from flatpak Brave + runs the
# extractor once. Safe to re-run. The venv lives inside the org dir so it's
# scoped to this org, not the user's global site-packages.
#
# Why host-side: flatpak Brave's cookie DB + the user's GNOME keyring aren't
# reachable from inside the sandbox container. See extract_brave_cookies.py.
#
# Usage:
#   ./bootstrap.sh                # install + extract
#   ./bootstrap.sh --check        # only validate, don't write cookies
#   ./bootstrap.sh --no-extract   # only install the venv

set -euo pipefail

# bootstrap.sh lives at the org root. ORG_DIR is its own directory.
ORG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$ORG_DIR/.venv"
REQS="$ORG_DIR/requirements.txt"

if ! command -v uv >/dev/null 2>&1; then
  echo "ERROR: uv not installed. Install: curl -LsSf https://astral.sh/uv/install.sh | sh" >&2
  exit 1
fi

# Idempotent venv create
if [ ! -d "$VENV_DIR" ]; then
  echo ">> Creating venv at $VENV_DIR"
  uv venv "$VENV_DIR" --python 3.12
else
  echo ">> venv exists, skipping create"
fi

# Idempotent dep install. uv syncs to requirements.txt — only reinstalls on drift.
echo ">> Syncing deps from $REQS"
uv pip install --python "$VENV_DIR/bin/python" -r "$REQS"

# Verify
"$VENV_DIR/bin/python" -c "import browser_cookie3, Cryptodome.Cipher.AES, jeepney; print('deps OK')"

# Use the shared extractor — supports any browser + domain.
# Twitter cookies come from flatpak Brave; output goes to the path twitter_session.py expects.
EXTRACTOR="$ORG_DIR/../_shared/sandbox/extract_browser_cookies.py"
OUT_PATH="$ORG_DIR/data/.twitter-session.json"

case "${1:-}" in
  --check)
    echo ">> Validating cookies only"
    "$VENV_DIR/bin/python" "$EXTRACTOR" --browser brave --domain x.com --check
    ;;
  --no-extract)
    echo ">> Skipping cookie extraction (--no-extract)"
    ;;
  "" )
    echo ">> Extracting cookies"
    "$VENV_DIR/bin/python" "$EXTRACTOR" --browser brave --domain x.com --out "$OUT_PATH"
    ;;
  *)
    echo "Unknown arg: $1" >&2
    exit 1
    ;;
esac

echo ">> Done"
