#!/usr/bin/env bash
# scripts/dev.sh — Start Go backend + Vite frontend
# Sandboxes are created on-demand by the Go backend via Docker API.
# Press Ctrl+C to stop, then run 'task down' to fully clean up.
set -m  # job control so background processes get their own process groups

PORT="${1:-5174}"

# Kill whatever's on our ports before starting
fuser -k 3847/tcp 2>/dev/null || true
fuser -k ${PORT}/tcp 2>/dev/null || true
sleep 0.5

echo ""
echo "  Starting Go backend on :3847..."
(cd go-backend && go run cmd/server/main.go 2>&1) &
GO_PID=$!

echo "  Starting Vite on :${PORT}..."
(npm run dev -- --port "$PORT" 2>&1) &
VITE_PID=$!

# Wait for services to actually bind their ports
echo ""
for i in $(seq 1 30); do
  GO_UP=$(ss -tlnp 2>/dev/null | grep -c ":3847 " || true)
  VITE_UP=$(ss -tlnp 2>/dev/null | grep -c ":${PORT} " || true)
  if [ "$GO_UP" -ge 1 ] && [ "$VITE_UP" -ge 1 ]; then
    echo "  ✓ Backend  http://localhost:3847"
    echo "  ✓ Frontend http://localhost:${PORT}"
    echo ""
    echo "  Sandboxes will be created on-demand when you enable Computer Use."
    echo "  Press Ctrl+C to stop, then 'task down' to fully clean up."
    echo ""
    break
  fi
  sleep 1
done

# Keep script alive — wait for either process to exit
wait $GO_PID $VITE_PID 2>/dev/null
echo "A process exited. Stopping..."
kill $GO_PID $VITE_PID 2>/dev/null || true
