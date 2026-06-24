#!/usr/bin/env bash
# Video Production Org — bootstrap
#
# One-command setup:
#   cd orgs/video-production && ./bootstrap.sh
#
# This script:
#   1. Builds the video-producer container (Python + Manim + Kokoro + ffmpeg)
#   2. Starts it
#   3. Waits for health
#   4. Verifies all tools are present
#   5. Prints ready message
#
# Usage:
#   ./bootstrap.sh                # full bootstrap
#   ./bootstrap.sh --down         # tear down what bootstrap brought up

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Export the absolute project path so docker-compose can attach it both
# as the openshell.project-path label AND as the /sandbox/workspace bind
# mount. Pux's bash executor assumes /sandbox/workspace maps to the
# project root, and Pux's container discovery (FindSandboxByProject)
# uses the label to adopt this container instead of spinning up a
# sibling. Without this export, Pux creates its own container.
export OPENSHELL_PROJECT_PATH="${OPENSHELL_PROJECT_PATH:-$SCRIPT_DIR}"

# --down: inverse of up. Named volumes preserved (no -v).
# To wipe volumes too, run `docker compose down -v` manually.
if [[ "${1:-}" == "--down" ]]; then
  echo "=== Video Production Org — Tear Down ==="
  docker compose down
  echo "  Containers stopped."
  exit 0
fi

echo "=== Video Production Org Bootstrap ==="
echo "  OPENSHELL_PROJECT_PATH=$OPENSHELL_PROJECT_PATH"
echo ""

# --- Build & start ---
echo "[1/4] Building container (this may take a few minutes for LaTeX + Manim)..."
docker compose build --progress=plain 2>&1 | tail -5

echo ""
echo "[2/4] Starting container..."
docker compose up -d

# --- Wait for health ---
echo ""
echo "[3/4] Waiting for container to be healthy..."
for i in $(seq 1 60); do
    if docker compose ps | grep -q "healthy"; then
        echo "  Container healthy after ${i}s"
        break
    fi
    sleep 2
done

# --- Verify tools ---
echo ""
echo "[4/4] Verifying tools..."

verify() {
    local label="$1"
    shift
    if docker compose exec -T video-producer "$@" >/dev/null 2>&1; then
        echo "  ✓ $label"
    else
        echo "  ✗ $label (missing or broken)"
    fi
}

verify "Python 3.11"         python3 --version
verify "pip"                 pip --version
verify "ffmpeg"              ffmpeg -version
verify "ffprobe"             ffprobe -version
verify "pdftotext"           pdftotext -v
verify "espeak-ng"           espeak-ng --version
verify "Pillow"              python3 -c "from PIL import Image; print('ok')"
verify "manim"               python3 -c "import manim; print('manim', manim.__version__)"
verify "Kokoro TTS"          python3 /app/skills/scripts/synth_kokoro.py --check 2>/dev/null || echo "  ⚠ Kokoro may need additional setup (KOKORO_PYTHON or KOKORO_TTS_DIR)"

echo ""
echo "=== Bootstrap complete ==="
echo ""
echo "Video Producer is ready at: research-video-producer"
echo "Workspace: /workspace/video-productions/"
echo ""
echo "Usage examples:"
echo "  # Interactive shell"
echo "  docker compose exec video-producer bash"
echo ""
echo "  # Run a video job initialization"
echo "  docker compose exec video-producer python /app/skills/scripts/init_video_job.py \"My Topic\""
echo ""
echo "  # Check TTS setup"
echo "  docker compose exec video-producer python /app/skills/scripts/synth_kokoro.py --check"
echo ""
echo "  # Copy video out"
echo "  docker compose cp research-video-producer:/workspace/video-productions/jobs/<job>/exports/final.mp4 ."
echo ""
echo "Volumes (persist across restarts):"
echo "  research_video_prod_workspace → /workspace/video-productions"
