#!/usr/bin/env bash
# telegram-agent bootstrap — host-side, idempotent.
#
# Brings up the org's sandbox container, then points the operator at the
# Telethon session bootstrap (one-time, interactive — phone + SMS code).
# The session itself lives in the container at /sandbox/.telegram-session.session
# and is reused forever after the initial auth.
#
# Why host-side bootstrap.sh: the Telethon session bootstrap is interactive
# (SMS code prompt), which doesn't fit the agent loop. The operator runs it
# once via this script; afterwards every message-send is fully automated.
#
# Usage:
#   ./bootstrap.sh                          # full bootstrap
#   ./bootstrap.sh --check                  # verify only, don't start anything
#   ./bootstrap.sh --down                   # tear down what bootstrap brought up
#   ./bootstrap.sh --setup-credentials ID HASH PHONE
#                                           # write /sandbox/.telegram-credentials.json
#   ./bootstrap.sh --interactive-login      # run telegram_session.py --bootstrap

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

export OPENSHELL_PROJECT_PATH="${OPENSHELL_PROJECT_PATH:-$SCRIPT_DIR}"

log()  { printf '[bootstrap] %s\n' "$*"; }
ok()   { printf '[bootstrap] ✓ %s\n' "$*"; }
err()  { printf '[bootstrap] ERROR: %s\n' "$*" >&2; }

CONTAINER="telegram-agent-sandbox"

# Dispatch on the long-form subcommands first.
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

CHECK_ONLY=0
DO_DOWN=0
case "${1:-}" in
  --check) CHECK_ONLY=1 ;;
  --down)  DO_DOWN=1 ;;
  "") ;;
  *) err "unknown arg: $1"; exit 1 ;;
esac

# --down: inverse of up. Named volumes preserved (no -v).
if [ "$DO_DOWN" = "1" ]; then
  log "tearing down (docker compose down)"
  docker compose down
  ok "containers stopped"
  exit 0
fi

if [ "$CHECK_ONLY" = "1" ]; then
  log "--check requested; verifying container + session"
  if docker compose ps --status running --quiet | grep -q .; then
    ok "container running"
  else
    err "container not running — start with: $0"
    exit 1
  fi
  if docker compose exec -T "$CONTAINER" python3 /sandbox/telegram_session.py --check \
      >/dev/null 2>&1; then
    ok "telegram session alive"
  else
    err "session not alive — run: $0 --setup-credentials ID HASH PHONE  then  $0 --interactive-login"
    exit 1
  fi
  exit 0
fi

# 1. Detect gVisor (runsc) availability. See deep-research-engine/bootstrap.sh
#    for the full comment — same logic here.
if ! docker info 2>/dev/null | grep -qi 'runtimes:.*runsc'; then
  log "runsc not installed locally — writing docker-compose.override.yml"
  cat > docker-compose.override.yml <<'EOF'
# Local override — strips `runtime: runsc` for hosts without gVisor.
services:
  telegram-agent-sandbox:
    runtime: ""
EOF
fi

# 2. Bring up the sandbox container.
log "starting sandbox (docker compose up -d)"
docker compose up -d

# 3. Wait for running state.
log "waiting for container to be running"
for i in $(seq 1 30); do
  if docker compose ps --status running --quiet | grep -q .; then
    ok "container running after ${i}s"
    break
  fi
  sleep 1
done

# 4. Report session state — credentials + session file.
echo
echo "═══════════════════════════════════════════════════════════════"
echo "  telegram-agent bootstrap — container up"
echo "═══════════════════════════════════════════════════════════════"
echo "  Container:  $CONTAINER (restart: unless-stopped)"
echo "  Workspace:  $SCRIPT_DIR  (bind-mounted at /sandbox/workspace)"
echo

if docker compose exec -T "$CONTAINER" python3 /sandbox/telegram_session.py --check \
    >/dev/null 2>&1; then
  ok "session already alive — ready to send"
  exit 0
fi

echo "  Next steps (one-time interactive setup):"
echo "    1. Get api_id + api_hash from https://my.telegram.org/apps"
echo "    2. $0 --setup-credentials API_ID API_HASH PHONE"
echo "    3. $0 --interactive-login   # you'll get an SMS code"
echo
echo "  After the one-time setup, every post is fully automated:"
echo "    fire a prompt with --org telegram-agent"
echo "═══════════════════════════════════════════════════════════════"
