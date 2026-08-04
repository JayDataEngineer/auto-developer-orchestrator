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
# Rollback is via git history: server.py + scripts/start_pux_prod.sh were deleted
# in phase D (Aegra is the single AP runtime). Recover them by reverting the
# phase-D commits, then run server.py directly on :9988. Emergency-only — parity
# was proven before deletion (tests/upstream/ + live prod).
#
# KNOWN DELTA vs server.py (documented in AEGRA_PROD.md):
#  - persistence sqlite→Postgres (ephemeral threads do not migrate; acceptable).
#  - prepare/warmup: RESTORED. PrepareWarmupMiddleware (before_agent hook, armed
#    by runtime/upstream.py) now runs prepare() under Aegra — warmup_browser /
#    warmup_webhook fire once per run (warn-and-continue). See AEGRA_PROD.md #2.
#  - run-completion push still works: the EventBus (custom_app, mounted via
#    http.app) serves /events + /events/stream — Hermes's actual SSE consumption
#    model. Aegra has no EXTERNAL webhook, but the EventBus IS the receiver.
set -euo pipefail

# Derive PROJECT_ROOT from THIS script's location (parent of scripts/) so the
# default is correct on ANY host without editing. PUX_PROJECT_ROOT still wins as
# an explicit override (e.g. when Aegra runs on a separate host from the tree).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${PUX_PROJECT_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
unset SCRIPT_DIR
PUX_DIR="$PROJECT_ROOT/pux-harness"
# Services bind to 0.0.0.0 — accessible from localhost (tailscale serve)
# AND from Docker containers (Hermes reaches MCP via Tailscale IP).
# NEVER bind to a Tailscale IP: it breaks if the IP changes.
BIND_HOST="0.0.0.0"
API_PORT="${PUX_API_PORT:-9988}"
MCP_PORT="${PUX_MCP_PORT:-9987}"
LOG_DIR="$PROJECT_ROOT/.pux/logs"
PID_DIR="$PROJECT_ROOT/.pux/pids"
COMPOSE="$PROJECT_ROOT/scripts/aegra-prod.compose.yml"
ENV_TEMPLATE="$PROJECT_ROOT/scripts/aegra-prod.env.template"
SECRETS_ENV="$PROJECT_ROOT/.env"
mkdir -p "$LOG_DIR" "$PID_DIR"

export PUX_PROJECT_ROOT="$PROJECT_ROOT"
# Pin the sandbox edit target EXPLICITLY. Aegra is a single-project server
# (Hermes posts tasks; the agent edits ONE host tree). Without this, the
# sandbox's resolve_project_path() falls back to PUX_PROJECT_ROOT — correct
# HERE only because aegra runs on the orchestrator host, but the silent
# fallback is the same foot-gun that leaks edits across projects in the editor
# path. Make the bind explicit + overridable so a different project is a
# conscious choice, not an accident.
export PUX_PROJECT_PATH="${PUX_PROJECT_PATH:-$PROJECT_ROOT}"
export PUX_API_HOST="$BIND_HOST" PUX_API_PORT="$API_PORT"
export PUX_MCP_HOST="$BIND_HOST" PUX_MCP_PORT="$MCP_PORT"
export PUX_API_URL="http://127.0.0.1:$API_PORT"

# --- load secrets (project .env) + the aegra env template into THIS process so
#     the nohup'd children inherit them (aegra serve does not auto-load .env). ---
if [ -f "$ENV_TEMPLATE" ]; then set -a; . "$ENV_TEMPLATE"; set +a; fi
if [ -f "$SECRETS_ENV" ]; then set -a; . "$SECRETS_ENV"; set +a; fi
# PUX_UPSTREAM_GRAPH must be UNSET for prod (runtime/build_graph = real model).
unset PUX_UPSTREAM_GRAPH

case "${1:-start}" in
  stop)
    # Kill the whole PROCESS GROUP (setsid launch made the stored PID == PGID),
    # so uvicorn children die with the wrapper. Falls back to a direct kill,
    # then a port-derived sweep for any child that escaped its group (e.g. a
    # process launched before the setsid fix).
    for pf in serve mcp; do
      f="$PID_DIR/$pf.pid"
      [ -f "$f" ] || continue
      pid="$(cat "$f" 2>/dev/null || true)"
      if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        echo "[pux-aegra] stopping $pf (pgrp $pid)"
        kill -TERM -"$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
        sleep 2
        kill -KILL -"$pid" 2>/dev/null || kill -9 "$pid" 2>/dev/null || true
      fi
      rm -f "$f"
    done
    for port in "$API_PORT" "$MCP_PORT"; do
      hld="$(ss -ltnp 2>/dev/null | grep ":$port " | grep -oP 'pid=\K[0-9]+' | head -1)"
      if [ -n "$hld" ]; then
        echo "[pux-aegra] killing lingering :$port holder pid $hld"
        kill "$hld" 2>/dev/null || true; sleep 1; kill -9 "$hld" 2>/dev/null || true
      fi
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
      echo "[pux-aegra] stopping prior $name (pgrp $pid)"
      kill -TERM -"$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
      sleep 2
      kill -KILL -"$pid" 2>/dev/null || kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$pf"
  fi
}
stop_pidfile "$PID_DIR/serve.pid" "aegra serve"
stop_pidfile "$PID_DIR/mcp.pid" "mcp"

start_one() {
  local name="$1" cmd="$2" pf="$3" lf="$4"
  echo "[pux-aegra] starting $name → $lf"
  # setsid: the launched process becomes a NEW session/group leader whose PID
  # (stored) == its PGID, so `kill -- -PID` reaches the WHOLE tree. This matters
  # because `aegra serve` spawns uvicorn as a CHILD; killing only the nohup
  # wrapper PID orphans uvicorn holding the port (caused a stuck :9988 on the
  # first reversibility attempt). See AEGRA_PROD.md.
  setsid nohup bash -c "$cmd" >"$lf" 2>&1 &
  echo $! > "$pf"
}

# aegra serve (AP HTTP) — prod mode, Tailscale-bound, no reload. Must be up
# before mcp wraps it. Reads pux-harness/aegra.json (custom_app mounted).
# `--extra prod`: aegra lives in the ``prod`` optional-dependency (kept out of the
# base install). ``uv run --extra prod`` installs it on first start AND re-installs
# it if a bare ``uv sync`` pruned it — so prod self-heals without manual reinstall.
start_one serve \
  "cd '$PUX_DIR' && PUX_PROJECT_ROOT='$PROJECT_ROOT' AEGRA_CONFIG=aegra.json exec uv run --extra prod aegra serve --host '$BIND_HOST' --port '$API_PORT'" \
  "$PID_DIR/serve.pid" "$LOG_DIR/aegra-serve.log"

# Wait for Aegra readiness via the custom_app EventBus endpoint (mounted via
# http.app) — proves the AP runtime + pux surfaces are both live.
echo "[pux-aegra] waiting for aegra /events/health on $BIND_HOST:$API_PORT ..."
for i in $(seq 1 90); do
  if curl -fsS "http://$BIND_HOST:$API_PORT/events/health" 2>/dev/null | grep -q '"ok"'; then
    echo "[pux-aegra] aegra healthy after ${i}s"; break
  fi
  sleep 1
  if [ "$i" = 90 ]; then echo "[pux-aegra] WARN: aegra not healthy in 90s — check $LOG_DIR/aegra-serve.log"; fi
done

# mcp (FastMCP SSE wrapper) — unchanged; proxies the Aegra AP backend.
start_one mcp \
  "cd '$PUX_DIR' && PUX_PROJECT_ROOT='$PROJECT_ROOT' PUX_MCP_HOST='$BIND_HOST' PUX_MCP_PORT='$MCP_PORT' PUX_API_URL='http://127.0.0.1:$API_PORT' exec uv run python -m pux_harness mcp" \
  "$PID_DIR/mcp.pid" "$LOG_DIR/mcp.log"

sleep 3
echo "[pux-aegra] stack up:"
echo "  aegra pid $(cat "$PID_DIR/serve.pid") → http://$BIND_HOST:$API_PORT  (log $LOG_DIR/aegra-serve.log)"
echo "  mcp   pid $(cat "$PID_DIR/mcp.pid") → http://$BIND_HOST:$MCP_PORT  (log $LOG_DIR/mcp.log)"
echo "[pux-aegra] rollback is via git history (server.py deleted in phase D); see docs/AEGRA_PROD.md"
