#!/usr/bin/env bash
# bootstrap.sh — idempotent deploy of invest org sandbox → target project.
#
# Usage:
#   bash orgs/invest/bootstrap.sh                              # default target
#   bash orgs/invest/bootstrap.sh /path/to/project             # custom target
#   bash orgs/invest/bootstrap.sh /path/to/project --no-venv   # skip venv setup
#
# What it does:
#   1. Creates target dir structure if missing
#   2. Copies sandbox/* + config/* (only if changed — sha256 compare)
#   3. Sets up Python venv via uv (idempotent)
#   4. Installs requirements
#   5. Health check: every .py file compiles, --help works
#   6. Prints summary
#
# Exits 0 on success, non-zero on failure. Safe to re-run.

set -euo pipefail

# ── Args ──────────────────────────────────────────────────────────────────
TARGET_DIR="${1:-${INVEST_TARGET_DIR:-$HOME/Documents/programs/dev/invest}}"
SKIP_VENV="${2:-}"

# Resolve the org source dir (relative to this script)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_SANDBOX="$SCRIPT_DIR/sandbox"
SRC_CONFIG="$SCRIPT_DIR/config"
REQUIREMENTS="$SCRIPT_DIR/requirements.txt"

# ── Helpers ───────────────────────────────────────────────────────────────
log()  { printf '[bootstrap] %s\n' "$*"; }
err()  { printf '[bootstrap] ERROR: %s\n' "$*" >&2; }
ok()   { printf '[bootstrap] ✓ %s\n' "$*"; }

# ── Pre-flight ────────────────────────────────────────────────────────────
if [[ ! -d "$SRC_SANDBOX" ]]; then
  err "Source sandbox not found: $SRC_SANDBOX"
  exit 1
fi
if [[ ! -f "$REQUIREMENTS" ]]; then
  err "requirements.txt not found: $REQUIREMENTS"
  exit 1
fi

mkdir -p "$TARGET_DIR/sandbox" "$TARGET_DIR/config"
ok "Target dir ready: $TARGET_DIR"

# ── Sync sandbox/ + config/ (skip unchanged via sha256) ───────────────────
synced=0
skipped=0
for src in "$SRC_SANDBOX"/*; do
  [[ -f "$src" ]] || continue
  fname="$(basename "$src")"
  dst="$TARGET_DIR/sandbox/$fname"
  if [[ -f "$dst" ]] && [[ "$(sha256sum < "$src")" == "$(sha256sum < "$dst")" ]]; then
    skipped=$((skipped+1))
  else
    cp "$src" "$dst"
    synced=$((synced+1))
  fi
done

for src in "$SRC_CONFIG"/*; do
  [[ -f "$src" ]] || continue
  fname="$(basename "$src")"
  dst="$TARGET_DIR/config/$fname"
  if [[ -f "$dst" ]] && [[ "$(sha256sum < "$src")" == "$(sha256sum < "$dst")" ]]; then
    skipped=$((skipped+1))
  else
    cp "$src" "$dst"
    synced=$((synced+1))
  fi
done

ok "Sandbox synced: $synced files updated, $skipped unchanged"

# ── Python venv (uv) ──────────────────────────────────────────────────────
VENV_DIR=""
if [[ "$SKIP_VENV" == "--no-venv" ]]; then
  log "Skipping venv setup (--no-venv)"
elif ! command -v uv >/dev/null 2>&1; then
  err "uv not found. Install: curl -LsSf https://astral.sh/uv/install.sh | sh"
  err "Or rerun with: bash $0 $TARGET_DIR --no-venv"
  exit 1
else
  VENV_DIR="$TARGET_DIR/.venv"
  if [[ ! -d "$VENV_DIR" ]]; then
    log "Creating venv at $VENV_DIR"
    uv venv "$VENV_DIR" >/dev/null
    ok "venv created"
  fi
  log "Installing requirements (idempotent)"
  uv pip install --python "$VENV_DIR/bin/python" -r "$REQUIREMENTS" >/dev/null 2>&1 || {
    err "pip install failed. Run manually: uv pip install --python $VENV_DIR/bin/python -r $REQUIREMENTS"
    exit 1
  }
  ok "Dependencies installed"
fi

# ── Health check: every .py compiles ──────────────────────────────────────
PY="${VENV_DIR:+$VENV_DIR/bin/python}"
[[ -z "$PY" ]] && PY="python3"

compile_failures=0
for f in "$TARGET_DIR/sandbox"/*.py; do
  if ! "$PY" -m py_compile "$f" 2>/dev/null; then
    err "Compile failed: $(basename "$f")"
    compile_failures=$((compile_failures+1))
  fi
done

if [[ $compile_failures -gt 0 ]]; then
  err "$compile_failures file(s) failed to compile"
  exit 1
fi
ok "All Python files compile"

# ── Health check: fetch_data --help ───────────────────────────────────────
if ! "$PY" "$TARGET_DIR/sandbox/fetch_data.py" --help >/dev/null 2>&1; then
  err "fetch_data.py --help failed"
  exit 1
fi
ok "fetch_data.py CLI functional"

# ── Summary ───────────────────────────────────────────────────────────────
echo
echo "═══════════════════════════════════════════════════════════════"
echo "  invest org bootstrap — COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo "  Target:       $TARGET_DIR"
echo "  Sandbox:      $TARGET_DIR/sandbox/ ($(ls "$TARGET_DIR/sandbox"/*.py 2>/dev/null | wc -l) .py files)"
echo "  Config:       $TARGET_DIR/config/"
if [[ -d "$VENV_DIR" ]]; then
  echo "  Venv:         $VENV_DIR"
  echo "  Activate:     source $VENV_DIR/bin/activate"
fi
echo "  Watchlist:    $TARGET_DIR/config/watchlist.json (multi-asset)"
echo
echo "  Required env:"
echo "    ALPACA_API_KEY, ALPACA_SECRET_KEY  (paper trading)"
echo "    FRED_API_KEY                        (macro data — free at fred.stlouisfed.org)"
echo
echo "  Docker compose (optional alternative to venv):"
echo "    export OPENSHELL_PROJECT_PATH=\$TARGET_DIR"
echo "    cd $SCRIPT_DIR && docker compose up -d"
echo "    # Pux will then adopt this container instead of creating a sibling."
echo
echo "  Smoke test passed: ✓"
echo "═══════════════════════════════════════════════════════════════"
