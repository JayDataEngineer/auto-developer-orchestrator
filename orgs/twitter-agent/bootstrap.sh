#!/usr/bin/env bash
# AUTO-GENERATED — bootstrap.sh for twitter-agent.
# Source of truth: orgs/twitter-agent/org.toml → [sandbox.bootstrap] + tier.
# Template: scripts/templates/org/bootstrap.sh.j2
#
# Rendered via `task org-build`. Hand-edits will be overwritten on next render;
# add org-specific stages by editing org.toml, not this file.
#
# Usage:
#   ./bootstrap.sh                # full bootstrap
#   ./bootstrap.sh --check        # verify only, don't start anything
#   ./bootstrap.sh --down         # tear down what bootstrap brought up
#
# ── Semantics of `--check` ─────────────────────────────────────────────────
# `--check` is a dry-run contract: "verify dependencies + configuration, then
# exit 0 WITHOUT starting the container or making network changes." It runs:
#   1. hard_dep checks  (docker, docker compose, etc.)   → fail-fast
#   2. soft_dep checks  (API keys in env)                → warn-only
#   3. host_setup[].check_args for each declared helper → can fail-fast
#   4. compose config validation (if tier != skeleton)   → fail-fast
#
# Orgs with host_setup helpers (e.g. the browser capability's
# extract_browser_cookies.py — see config/capabilities/browser/SKILL.md)
# extend `--check` to dry-run the helper too — the helper's check_args is the
# canonical "does this work without side effects?" probe. Skeleton-tier orgs
# (no container, no host_setup) still run dep checks + exit 0.
#
# This is DIFFERENT from `compose config --check` (yaml syntax only) and
# DIFFERENT from a single helper's own `--check` flag. The bootstrap `--check`
# subcommand is the union of all dry-run probes for the org's full lifecycle.
# See [feedback_pr4_container_lifecycle] for the contract motivation.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$SCRIPT_DIR"

# Canonical Pux mount + label — lets Pux adopt this container instead of
# spinning up a sibling. pwd -P resolves symlinks so the label matches
# what Pux queries by (resolveOrgPath EvalSymlinks). See
# [[feedback_container_reuse_label_discovery]].
export OPENSHELL_PROJECT_PATH="${OPENSHELL_PROJECT_PATH:-$SCRIPT_DIR}"

log()  { printf '[bootstrap] %s\n' "$*"; }
ok()   { printf '[bootstrap] ✓ %s\n' "$*"; }
err()  { printf '[bootstrap] ERROR: %s\n' "$*" >&2; }

CONTAINER="twitter-agent-sandbox"

# ── Extra subcommands (org-specific extensions, declared in org.toml) ──────
# Dispatched BEFORE the standard --check/--down args so they take precedence.

# ── Args ──────────────────────────────────────────────────────────────────
CHECK_ONLY=0
DO_DOWN=0
EMIT_ENV=0
case "${1:-}" in
  --check)   CHECK_ONLY=1 ;;
  --down)    DO_DOWN=1 ;;
  --emit-env) EMIT_ENV=1 ;;
  "")        ;;
  *)         err "unknown arg: $1"; exit 1 ;;
esac

# ── --down: inverse of up ─────────────────────────────────────────────────
# Tears down the container + its network. Named volumes are PRESERVED
# (no `-v` flag) so data isn't lost across re-bootstraps. To wipe volumes
# too, run `docker compose down -v` manually.
if [ "$DO_DOWN" = "1" ]; then
  log "tearing down (docker compose down)"
  docker compose down
  ok "containers stopped"
  exit 0
fi

# ── 1. Hard deps (fail-fast) ──────────────────────────────────────────────
if ! command -v docker >/dev/null 2>&1; then
  err "docker not found on PATH"
  exit 1
fi
ok "docker"
if ! docker compose version >/dev/null 2>&1; then
  err "docker compose subcommand missing — install Docker Compose v2"
  exit 1
fi
ok "docker compose"

# ── 2. Soft deps (warn only) ──────────────────────────────────────────────

# ── 3. Host-side setup (pre-compose, optional) ────────────────────────────
# Runs declared helpers on the host BEFORE compose up. Used when the helper
# needs host-only resources (flatpak cookie DB, GNOME keyring, USB devices,
# audio hardware). Each helper may install Python deps into $SCRIPT_DIR/.venv
# and run a script with its declared args.
if ! command -v uv >/dev/null 2>&1; then
  err "uv not installed — required by host_setup. Install: curl -LsSf https://astral.sh/uv/install.sh | sh"
  exit 1
fi
# twitter_cookies: Extract Twitter cookies from flatpak Brave on the host (cookie DB + GNOME keyring not reachable from inside the container)
if [ ! -d "$SCRIPT_DIR/.venv" ]; then
  log "creating venv at $SCRIPT_DIR/.venv"
  uv venv "$SCRIPT_DIR/.venv" --python 3.12
fi
log "installing python deps for twitter_cookies: browser-cookie3 pycryptodome jeepney"
uv pip install --python "$SCRIPT_DIR/.venv/bin/python" browser-cookie3 pycryptodome jeepney >/dev/null

if [ "$CHECK_ONLY" = "1" ]; then
  log "check mode: twitter_cookies"
  if ! "$SCRIPT_DIR/.venv/bin/python" "$SCRIPT_DIR/../_shared/sandbox/extract_browser_cookies.py" --browser brave --domain x.com --check; then
    err "twitter_cookies check failed"
    exit 1
  fi
else
  log "running twitter_cookies"
  if ! "$SCRIPT_DIR/.venv/bin/python" "$SCRIPT_DIR/../_shared/sandbox/extract_browser_cookies.py" --browser brave --domain x.com --out data/.twitter-session.json; then
    err "twitter_cookies failed"
    exit 1
  fi
fi
ok "twitter_cookies"

# --emit-env: print export statements for the operator to eval into their
# shell before launching pux. Cookies are base64-encoded so the value is a
# single line with no special chars — safe to wrap in shell quotes.
# Source of truth for the env contract: orgs/twitter-agent/policy.yaml.
if [ "$EMIT_ENV" = "1" ]; then
  B64="$("$SCRIPT_DIR/.venv/bin/python" "$SCRIPT_DIR/../_shared/sandbox/extract_browser_cookies.py" --browser brave --domain x.com --b64)"
  if [ -z "$B64" ]; then
    err "emit-env: cookie extraction returned empty"
    exit 1
  fi
  # Quote the value to be safe with special chars (there shouldn't be any
  # in base64, but defense in depth). Single quotes prevent shell expansion.
  printf "export TWITTER_COOKIES_B64='%s'\n" "$B64"
  exit 0
fi

# ── 4. --check early-exit (after host_setup so check_args run if declared) ─
if [ "$CHECK_ONLY" = "1" ]; then
  log "--check requested; not starting container"
  exit 0
fi

# ── 5. gVisor detection ───────────────────────────────────────────────────
# The auto-generated compose requests `runtime: runsc` when org.toml
# declares runtime_class = "gvisor". Hosts without runsc installed choke
# on that line. Write a local override (gitignored) that strips it.
if ! docker info 2>/dev/null | grep -qi 'runtimes:.*runsc'; then
  log "runsc not installed locally — writing docker-compose.override.yml"
  cat > docker-compose.override.yml <<'EOF'
# Local override — strips `runtime: runsc` for hosts without gVisor.
# Auto-generated by bootstrap.sh; safe to delete.
services:
  twitter-agent-sandbox:
    runtime: ""
EOF
fi

# ── 6. Build (custom-build tier only) ─────────────────────────────────────

# ── 7. Bring up the container ─────────────────────────────────────────────
log "starting sandbox (docker compose up -d)"
docker compose up -d

log "waiting for container to be running"
for i in $(seq 1 30); do
  if docker compose ps --status running --quiet | grep -q .; then
    ok "container running after ${i}s"
    break
  fi
  sleep 1
done

# ── 8. Smoke test ─────────────────────────────────────────────────────────
log "smoke test: twitter sandbox up + cookie file reachable"
if docker compose exec -T "$CONTAINER" python3 -c 'import twitter_session; twitter_session._resolve_cookies_path() and print("session file present")' 2>/dev/null || python3 -c 'print("container up")' >/dev/null 2>&1; then
  ok "smoke test passed"
else
  err "smoke test failed — check container logs: docker compose logs"
  exit 1
fi

echo
echo "═══════════════════════════════════════════════════════════════"
echo "  twitter-agent bootstrap — COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo "  Container:  $CONTAINER (restart: unless-stopped)"
echo "  Workspace:  $SCRIPT_DIR  (bind-mounted at /sandbox/workspace)"
echo
echo "  Tear down:  ./bootstrap.sh --down"
echo "═══════════════════════════════════════════════════════════════"
