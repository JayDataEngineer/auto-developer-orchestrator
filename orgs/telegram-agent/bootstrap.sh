#!/usr/bin/env bash
# AUTO-GENERATED — bootstrap.sh for telegram-agent.
# Source of truth: orgs/telegram-agent/org.toml → [sandbox.bootstrap] + tier.
# Template: scripts/templates/org/bootstrap.sh.j2
#
# Rendered via `task org-build`. Hand-edits will be overwritten on next render;
# add org-specific stages by editing org.toml, not this file.
#
# Usage:
#   ./bootstrap.sh                # full bootstrap
#   ./bootstrap.sh --check        # verify only, don't start anything
#   ./bootstrap.sh --down         # tear down what bootstrap brought up
#   ./bootstrap.sh --setup-credentials API_ID API_HASH PHONE    # write /sandbox/.telegram-credentials.json (container-side)
#   ./bootstrap.sh --interactive-login     # run telegram_session.py --bootstrap (SMS code prompt)
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

CONTAINER="telegram-agent-sandbox"

# ── Extra subcommands (org-specific extensions, declared in org.toml) ──────
# Dispatched BEFORE the standard --check/--down args so they take precedence.
case "${1:-}" in
  --setup-credentials)
    if [ "$#" -ne 4 ]; then
      err "usage: $0 --setup-credentials API_ID API_HASH PHONE"
      exit 1
    fi
    log "writing credentials inside container"
    docker compose exec -T "$CONTAINER" python3 /sandbox/telegram_session.py \
      --setup-credentials "$2" "$3" "$4"
    ok "credentials written to /sandbox/.telegram-credentials.json"

    exit 0
    ;;
  --interactive-login)
    log "interactive login — you'll receive an SMS code"
    docker compose exec -it "$CONTAINER" python3 /sandbox/telegram_session.py --bootstrap
    ok "session created"

    exit 0
    ;;
esac

# ── Args ──────────────────────────────────────────────────────────────────
CHECK_ONLY=0
DO_DOWN=0
case "${1:-}" in
  --check) CHECK_ONLY=1 ;;
  --down)  DO_DOWN=1 ;;
  "")      ;;
  *)       err "unknown arg: $1"; exit 1 ;;
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
  telegram-agent-sandbox:
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
log "smoke test: telegram session alive"
if docker compose exec -T "$CONTAINER" python3 /sandbox/telegram_session.py --check >/dev/null 2>&1; then
  ok "smoke test passed"
else
  err "smoke test failed — check container logs: docker compose logs"
  exit 1
fi

echo
echo "═══════════════════════════════════════════════════════════════"
echo "  telegram-agent bootstrap — COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo "  Container:  $CONTAINER (restart: unless-stopped)"
echo "  Workspace:  $SCRIPT_DIR  (bind-mounted at /sandbox/workspace)"
echo
echo "  Tear down:  ./bootstrap.sh --down"
echo "═══════════════════════════════════════════════════════════════"
