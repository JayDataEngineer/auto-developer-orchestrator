#!/usr/bin/env bash
# PROD runner for the pux stack on AEGRA (the OSS langgraph-api drop-in) — the
# C2 cutover vehicle replacing scripts/start_pux_prod.sh (server.py).
#
# Topology (HOST-API): Aegra API on the host (systemd), postgres+redis as Docker
# sidecars (scripts/aegra-prod.compose.yml), both DBs on 127.0.0.1 ONLY. Same
# Tailscale binding + ports as server.py → consumers (Hermes /events+stream,
# dev-bot, MCP) need NO URL change on cutover (path-transparency, proven for the
# langgraph-api lane). The AP wire format is identical (langgraph_sdk).
#
#   aegra serve — Agent Protocol HTTP backend  :9988  (PUX_API_HOST)
#   pux mcp     — FastMCP SSE wrapper          :9987  (PUX_MCP_HOST)  [unchanged]
#
# Reversible: to roll back, `pux-aegra stop` (or systemctl stop) then re-run
# scripts/start_pux_prod.sh. server.py stays installed as the fallback runtime.
#
# KNOWN DELTA vs server.py (documented in AEGRA_PROD.md):
#  - persistence sqlite→Postgres (ephemeral threads do not migrate; acceptable).
#  - NO prepare/warmup hook in Aegra → warmup_browser/warmup_webhook do not fire
#    (browser orgs absorb Chrome cold-start on first run instead of pre-warm).
#  - run-completion push still works: the EventBus (custom_app, mounted via
#    http.app) serves /events + /events/stream — Hermes's actual SSE consumption
#    model. Aegra has no EXTERNAL webhook, but the EventBus IS the receiver.
set -euo pipefail

PROJECT_ROOT="${PUX_PROJECT_ROOT:-/home/ubuntu/Documents/programs/dev/auto-developer-orchestrator}"
PUX_DIR="$PROJECT_ROOT/pux-harness"
TS_IP="${PUX_TS_IP:-100.99.57.110}"
API_PORT="${PUX_API_PORT:-9988}"
MCP_PORT="${PUX_MCP_PORT:-9987}"
LOG_DIR="$PROJECT_ROOT/.pux/logs"
PID_DIR="$PROJECT_ROOT/.pux/pids"
COMPOSE="$PROJECT_ROOT/scripts/aegra-prod.compose.yml"
ENV_TEMPLATE="$PROJECT_ROOT/scripts/aegra-prod.env.template"
SECRETS_ENV="$PROJECT_ROOT/.env"
mkdir -p "$LOG_DIR" "$PID_DIR"

export PUX_PROJECT_ROOT="$PROJECT_ROOT"
export PUX_API_HOST="$TS_IP" PUX_API_PORT="$API_PORT"
export PUX_MCP_HOST="$TS_IP" PUX_MCP_PORT="$MCP_PORT"
export PUX_API_URL="http://$TS_IP:$API_PORT"

# --- load secrets (project .env) + the aegra env template into THIS process so
#     the nohup'd children inherit them (aegra serve does not auto-load .env). ---
if [ -f "$ENV_TEMPLATE" ]; then set -a; . "$ENV_TEMPLATE"; set +a; fi
if [ -f "$SECRETS_ENV" ]; then set -a; . "$SECRETS_ENV"; set +a; fi
# PUX_UPSTREAM_GRAPH must be UNSET for prod (runtime/build_graph = real model).
unset PUX_UPSTREAM_GRAPH

case "${1:-start}" in
  stop)
    for pf in serve mcp; do
      f="$PID_DIR/$pf.pid"
      [ -f "$f" ] || continue
      pid="$(cat "$f" 2>/dev/null || true)"
      if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        echo "[pux-aegra] stopping $pf (pid $pid)"; kill "$pid" 2>/dev/null || true
        sleep 2; kill -9 "$pid" 2>/dev/null || true
      fi
      rm -f "$f"
    done
    echo "[pux-aegra] (sidecars left running — 'docker compose -f $COMPOSE down' to remove DBs)"
    exit 0
    ;;
esac

# --- sidecars: postgres + redis (127.0.0.1 only). Idempotent. ---
echo "[pux-aegra] starting DB sidecars ($COMPOSE) ..."
docker compose -f "$COMPOSE" up -d postgres redis
echo "[pux-aegra] waiting for postgres healthy ..."
for i in $(seq 1 60); do
  if docker compose -f "$COMPOSE" ps postgres 2>/dev/null | grep -q "healthy\|running.*healthy"; then :; fi
  if docker exec pux-aegra-postgres pg_isready -U "${POSTGRES_USER:-pux_harness}" >/dev/null 2>&1; then
    echo "[pux-aegra] postgres ready after ${i}s"; break
  fi
  sleep 1
  if [ "$i" = 60 ]; then echo "[pux-aegra] WARN: postgres not ready in 60s — check docker logs"; fi
done

stop_pidfile() {
  local pf="$1" name="$2"
  if [ -f "$pf" ]; then
    local pid; pid="$(cat "$pf" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      echo "[pux-aegra] stopping prior $name (pid $pid)"; kill "$pid" 2>/dev/null || true
      sleep 2; kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$pf"
  fi
}
stop_pidfile "$PID_DIR/serve.pid" "aegra serve"
stop_pidfile "$PID_DIR/mcp.pid" "mcp"

start_one() {
  local name="$1" cmd="$2" pf="$3" lf="$4"
  echo "[pux-aegra] starting $name → $lf"
  nohup bash -c "$cmd" >"$lf" 2>&1 &
  echo $! > "$pf"
}

# aegra serve (AP HTTP) — prod mode, Tailscale-bound, no reload. Must be up
# before mcp wraps it. Reads pux-harness/aegra.json (custom_app mounted).
start_one serve \
  "cd '$PUX_DIR' && PUX_PROJECT_ROOT='$PROJECT_ROOT' AEGRA_CONFIG=aegra.json exec uv run aegra serve --host '$TS_IP' --port '$API_PORT'" \
  "$PID_DIR/serve.pid" "$LOG_DIR/aegra-serve.log"

# Wait for Aegra readiness via the custom_app EventBus endpoint (mounted via
# http.app) — proves the AP runtime + pux surfaces are both live.
echo "[pux-aegra] waiting for aegra /events/health on $TS_IP:$API_PORT ..."
for i in $(seq 1 90); do
  if curl -fsS "http://$TS_IP:$API_PORT/events/health" 2>/dev/null | grep -q '"ok"'; then
    echo "[pux-aegra] aegra healthy after ${i}s"; break
  fi
  sleep 1
  if [ "$i" = 90 ]; then echo "[pux-aegra] WARN: aegra not healthy in 90s — check $LOG_DIR/aegra-serve.log"; fi
done

# mcp (FastMCP SSE wrapper) — unchanged; proxies the Aegra AP backend.
start_one mcp \
  "cd '$PUX_DIR' && PUX_PROJECT_ROOT='$PROJECT_ROOT' PUX_MCP_HOST='$TS_IP' PUX_MCP_PORT='$MCP_PORT' PUX_API_URL='http://$TS_IP:$API_PORT' exec uv run python -m pux_harness mcp" \
  "$PID_DIR/mcp.pid" "$LOG_DIR/mcp.log"

sleep 3
echo "[pux-aegra] stack up:"
echo "  aegra pid $(cat "$PID_DIR/serve.pid") → http://$TS_IP:$API_PORT  (log $LOG_DIR/aegra-serve.log)"
echo "  mcp   pid $(cat "$PID_DIR/mcp.pid") → http://$TS_IP:$MCP_PORT  (log $LOG_DIR/mcp.log)"
echo "[pux-aegra] rollback: $0 stop  &&  scripts/start_pux_prod.sh"
