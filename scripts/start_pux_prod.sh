#!/usr/bin/env bash
# PROD runner for the pux Agent Protocol stack — phone(telegram)→Hermes(@cloud)→
# MCP→dev-bot(@ubuntu-desktop). Two processes, both bound to the Tailscale IP
# ONLY (reachable from Tailscale peers like cloud, NOT the LAN/public):
#
#   pux serve  — Agent Protocol HTTP backend  :9988  (PUX_API_HOST)
#   pux mcp    — FastMCP SSE wrapper          :9987  (PUX_MCP_HOST)
#
# Hermes(@cloud) points an mcp_servers: url: entry at http://<tailscale-ip>:9987.
# dev-bot runs in its bridged container (host-net → Tailscale identity → cross-ssh).
#
# Keys: .env at PROJECT_ROOT carries ANTHROPIC_AUTH_TOKEN (ZAI glm-5.2) +
# OPENCODE_API_KEY (mimo); the kit bootstrap loads it. Re-run is idempotent-ish
# (kills prior PIDs in the pidfiles first). Logs go to .pux/logs/.
set -euo pipefail

PROJECT_ROOT="${PUX_PROJECT_ROOT:-/home/ubuntu/Documents/programs/dev/auto-developer-orchestrator}"
PUX_DIR="$PROJECT_ROOT/pux-harness"
TS_IP="${PUX_TS_IP:-100.99.57.110}"
API_PORT="${PUX_API_PORT:-9988}"
MCP_PORT="${PUX_MCP_PORT:-9987}"
LOG_DIR="$PROJECT_ROOT/.pux/logs"
PID_DIR="$PROJECT_ROOT/.pux/pids"
mkdir -p "$LOG_DIR" "$PID_DIR"

export PUX_PROJECT_ROOT="$PROJECT_ROOT"
export PUX_API_HOST="$TS_IP" PUX_API_PORT="$API_PORT"
export PUX_MCP_HOST="$TS_IP" PUX_MCP_PORT="$MCP_PORT"
export PUX_API_URL="http://$TS_IP:$API_PORT"

# Kill a prior instance from its pidfile (best-effort).
stop_pidfile() {
  local pf="$1" name="$2"
  if [ -f "$pf" ]; then
    local pid; pid="$(cat "$pf" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      echo "[pux-prod] stopping prior $name (pid $pid)"
      kill "$pid" 2>/dev/null || true
      sleep 2
      kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$pf"
  fi
}
stop_pidfile "$PID_DIR/serve.pid" "serve"
stop_pidfile "$PID_DIR/mcp.pid" "mcp"

start_one() {
  local name="$1" cmd="$2" pf="$3" lf="$4"
  echo "[pux-prod] starting $name → $lf"
  nohup bash -c "$cmd" >"$lf" 2>&1 &
  echo $! > "$pf"
}

# serve (AP HTTP) — must be up before mcp wraps it.
start_one serve \
  "cd '$PUX_DIR' && PUX_PROJECT_ROOT='$PROJECT_ROOT' PUX_API_HOST='$TS_IP' PUX_API_PORT='$API_PORT' exec uv run python -m pux_harness serve" \
  "$PID_DIR/serve.pid" "$LOG_DIR/serve.log"

# Wait for serve /ok before starting mcp (mcp proxies it).
echo "[pux-prod] waiting for serve health on $TS_IP:$API_PORT ..."
for i in $(seq 1 60); do
  if curl -fsS "http://$TS_IP:$API_PORT/ok" >/dev/null 2>&1; then
    echo "[pux-prod] serve healthy after ${i}s"; break
  fi
  sleep 1
  if [ "$i" = 60 ]; then echo "[pux-prod] WARN: serve not healthy in 60s — check $LOG_DIR/serve.log"; fi
done

# mcp (FastMCP SSE wrapper).
start_one mcp \
  "cd '$PUX_DIR' && PUX_PROJECT_ROOT='$PROJECT_ROOT' PUX_MCP_HOST='$TS_IP' PUX_MCP_PORT='$MCP_PORT' PUX_API_URL='http://$TS_IP:$API_PORT' exec uv run python -m pux_harness mcp" \
  "$PID_DIR/mcp.pid" "$LOG_DIR/mcp.log"

sleep 3
echo "[pux-prod] stack up:"
echo "  serve pid $(cat "$PID_DIR/serve.pid") → http://$TS_IP:$API_PORT  (log $LOG_DIR/serve.log)"
echo "  mcp   pid $(cat "$PID_DIR/mcp.pid") → http://$TS_IP:$MCP_PORT  (log $LOG_DIR/mcp.log)"
