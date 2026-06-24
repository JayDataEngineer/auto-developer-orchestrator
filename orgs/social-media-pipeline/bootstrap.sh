#!/usr/bin/env bash
# social-media-pipeline bootstrap — host-side, idempotent.
#
# Brings up the org's sandbox container and verifies host-side deps
# (SurrealDB). The actual platform credentials (Twitter cookies, Telegram
# session) are handled by the shared sandbox scripts that ship as init_files
# — this script just gets the container running and reachable.
#
# Usage:
#   ./bootstrap.sh                # full bootstrap
#   ./bootstrap.sh --check        # verify only, don't start anything
#   ./bootstrap.sh --down         # tear down what bootstrap brought up

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

export OPENSHELL_PROJECT_PATH="${OPENSHELL_PROJECT_PATH:-$SCRIPT_DIR}"

log()  { printf '[bootstrap] %s\n' "$*"; }
ok()   { printf '[bootstrap] ✓ %s\n' "$*"; }
err()  { printf '[bootstrap] ERROR: %s\n' "$*" >&2; }

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

# 1. Host-side SurrealDB.
SURREALDB_HEALTH="${SURREALDB_HEALTH:-http://localhost:8000/surreal/health}"
log "checking SurrealDB at $SURREALDB_HEALTH"
if curl -sf -o /dev/null "$SURREALDB_HEALTH"; then
  ok "SurrealDB reachable"
else
  err "SurrealDB not reachable at $SURREALDB_HEALTH"
  exit 1
fi

if [ "$CHECK_ONLY" = "1" ]; then
  log "--check requested; not starting container"
  exit 0
fi

# 2. Detect gVisor (runsc) availability. See deep-research-engine/bootstrap.sh
#    for the full comment — same logic here.
if ! docker info 2>/dev/null | grep -qi 'runtimes:.*runsc'; then
  log "runsc not installed locally — writing docker-compose.override.yml"
  cat > docker-compose.override.yml <<'EOF'
# Local override — strips `runtime: runsc` for hosts without gVisor.
services:
  social-media-pipeline-sandbox:
    runtime: ""
EOF
fi

# 3. Bring up the sandbox container.
log "starting sandbox (docker compose up -d)"
docker compose up -d

# 4. Wait for running state.
log "waiting for container to be running"
for i in $(seq 1 30); do
  if docker compose ps --status running --quiet | grep -q .; then
    ok "container running after ${i}s"
    break
  fi
  sleep 1
done

# 5. Smoke test: sandbox → SurrealDB (host network mode → localhost).
CONTAINER="social-media-pipeline-sandbox"
log "smoke test: container → SurrealDB"
if docker compose exec -T "$CONTAINER" \
    python3 -c "import os,urllib.request; url=os.environ['SURREALDB_URL'].replace('host.docker.internal','localhost'); urllib.request.urlopen(url+'/health').read(); print('ok')" \
    >/dev/null 2>&1; then
  ok "container → SurrealDB reachable"
else
  err "container cannot reach SurrealDB"
  exit 1
fi

echo
echo "═══════════════════════════════════════════════════════════════"
echo "  social-media-pipeline bootstrap — COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo "  Container:  $CONTAINER (restart: unless-stopped)"
echo "  SurrealDB:  $SURREALDB_HEALTH"
echo "  Workspace:  $SCRIPT_DIR  (bind-mounted at /sandbox/workspace)"
echo
echo "  Next steps (agent-side):"
echo "    fire a prompt with --org social-media-pipeline"
echo "    the agent will load platform credentials from /sandbox/workspace/data/"
echo "═══════════════════════════════════════════════════════════════"
