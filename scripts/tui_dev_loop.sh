#!/usr/bin/env bash
# TUI Autonomous Development Loop
#
# Starts the visual testing server and Go backend, providing a complete
# environment for autonomous TUI development with visual feedback.
#
# Usage:
#   source /tmp/tui-venv/bin/activate   # pyte + Pillow
#   bash scripts/tui_dev_loop.sh        # starts everything
#
# Then from another terminal (or from an AI agent):
#   curl http://localhost:9877/screenshot > shot.png    # see the TUI
#   curl -X POST localhost:9877/input -d '{"text":"hi"}'  # type into it
#   curl -X POST localhost:9877/restart                   # apply code changes

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VENV="$ROOT/.venv/tui-visual"

# Check venv
if [ ! -d "$VENV" ]; then
    echo "Creating venv and installing dependencies..."
    uv venv "$VENV"
    uv pip install --python "$VENV/bin/python" pyte Pillow
fi

# Kill any existing instances
pkill -f "tui_visual.py" 2>/dev/null || true
sleep 0.5

# Start Go backend if not running
if ! curl -s http://localhost:3847/api/health > /dev/null 2>&1; then
    echo "Starting Go backend..."
    (cd "$ROOT/go-backend" && go run ./cmd/server/ > /tmp/orch-backend.log 2>&1) &
    BACKEND_PID=$!
    echo "Backend PID: $BACKEND_PID (waiting for ready...)"
    for i in $(seq 1 30); do
        if curl -s http://localhost:3847/api/health > /dev/null 2>&1; then
            echo "Backend ready!"
            break
        fi
        sleep 1
    done
else
    echo "Go backend already running on :3847"
fi

# Start visual server
echo "Starting TUI visual server on :9877..."
cd "$ROOT"
"$VENV/bin/python" scripts/tui_visual.py --port 9877 --cols 120 --rows 40 --font-size 14 &
VISUAL_PID=$!

sleep 2

# Verify
if curl -s http://localhost:9877/health > /dev/null 2>&1; then
    echo ""
    echo "========================================="
    echo "  TUI Dev Environment Ready"
    echo "========================================="
    echo ""
    echo "  Visual Server:  http://localhost:9877"
    echo "  Screenshot:     curl -s localhost:9877/screenshot -o shot.png"
    echo "  Screen text:    curl -s localhost:9877/screen"
    echo "  Send input:     curl -X POST localhost:9877/input -H 'Content-Type: application/json' -d '{\"text\":\"hello\\n\"}'"
    echo "  Restart TUI:    curl -X POST localhost:9877/restart"
    echo "  Health:         curl -s localhost:9877/health"
    echo ""
    echo "  Backend:        http://localhost:3847"
    echo ""
    echo "  PIDs: visual=$VISUAL_PID"
    echo ""
    echo "  Ctrl+C to stop everything"
    echo "========================================="

    # Wait for Ctrl+C
    wait $VISUAL_PID
else
    echo "Visual server failed to start!"
    exit 1
fi
