#!/usr/bin/env bash
# tech-noir bootstrap — host-side, idempotent.
#
# Brings up the org's sandbox container and verifies host-side deps
# (SurrealDB, optional Godot MCP + ComfyUI). Safe to re-run.
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

# 1. Host-side SurrealDB (hard dep).
SURREALDB_HEALTH="${SURREALDB_HEALTH:-http://localhost:8000/surreal/health}"
log "checking SurrealDB at $SURREALDB_HEALTH"
if curl -sf -o /dev/null "$SURREALDB_HEALTH"; then
  ok "SurrealDB reachable"
else
  err "SurrealDB not reachable at $SURREALDB_HEALTH"
  exit 1
fi

# 2. Optional creative-stack deps — soft checks.
GODOT_MCP_URL="${GODOT_MCP_URL:-http://localhost:8080}"
COMFYUI_URL="${COMFYUI_URL:-http://localhost:8188}"
for url in "$GODOT_MCP_URL" "$COMFYUI_URL"; do
  if curl -sf -o /dev/null --max-time 2 "$url"; then
    ok "creative-stack reachable: $url"
  else
    log "warning: creative-stack not reachable: $url (game-dev / image-gen paths may degrade)"
  fi
done

if [ "$CHECK_ONLY" = "1" ]; then
  log "--check requested; not starting container"
  exit 0
fi

# 3. Detect gVisor (runsc) availability. See deep-research-engine/bootstrap.sh
#    for the full comment — same logic here.
if ! docker info 2>/dev/null | grep -qi 'runtimes:.*runsc'; then
  log "runsc not installed locally — writing docker-compose.override.yml"
  cat > docker-compose.override.yml <<'EOF'
# Local override — strips `runtime: runsc` for hosts without gVisor.
services:
  tech-noir-sandbox:
    runtime: ""
EOF
fi

# 4. Bring up the sandbox container.
log "starting sandbox (docker compose up -d)"
docker compose up -d

# 5. Wait for running state.
log "waiting for container to be running"
for i in $(seq 1 30); do
  if docker compose ps --status running --quiet | grep -q .; then
    ok "container running after ${i}s"
    break
  fi
  sleep 1
done

# 6. Smoke test: sandbox → SurrealDB (host network mode → localhost).
CONTAINER="tech-noir-sandbox"
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
echo "  tech-noir bootstrap — COMPLETE"
echo "═══════════════════════════════════════════════════════════════"
echo "  Container:  $CONTAINER (restart: unless-stopped)"
echo "  SurrealDB:  $SURREALDB_HEALTH"
echo "  Workspace:  $SCRIPT_DIR  (bind-mounted at /sandbox/workspace)"
echo
echo "  Next steps (agent-side):"
echo "    fire a prompt with --org tech-noir"
echo "═══════════════════════════════════════════════════════════════"
