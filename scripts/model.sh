#!/usr/bin/env bash
# Start optimized llama.cpp server for agent workloads.
#
# Modes:
#   ./scripts/model.sh              # default: vision + thinking + cont-batching
#   ./scripts/model.sh fast         # vision, no thinking (faster per-token)
#   ./scripts/model.sh text         # text-only + thinking (slightly more VRAM for context)
#   ./scripts/model.sh bare         # text-only, no thinking (minimal VRAM)
#
# Benchmarked on RTX 4090 (Apr 2026):
#   Baseline (vision, thinking, parallel=2):  ~118 tok/s
#   Speculative decoding with E2B draft:      ~93 tok/s  (SLOWER — draft rejection overhead)
#   CPU-MoE:                                   ~14 tok/s  (catastrophic — avoid)
#
# Sampling defaults (per Unsloth): temperature=1.0, top_p=0.95, top_k=64
set -euo pipefail

MODELS_DIR="${MODELS_DIR:-/home/ubuntu/Documents/programs/shared-docker-infra/models/llm}"
VISION_DIR="${VISION_DIR:-/home/ubuntu/Documents/programs/shared-docker-infra/models/vision}"
LLAMA_IMAGE="ghcr.io/ggml-org/llama.cpp:server-cuda"
LLAMA_PORT="${LLAMA_PORT:-8001}"
FLAGS="${1:-default}"

# Stop any existing container
docker rm -f orchestrator-llama 2>/dev/null || true

case "$FLAGS" in
  bare)
    echo "Starting Gemma-4-26B (bare — text-only, no thinking)..."
    docker run -d --name orchestrator-llama \
      --gpus all \
      --network shared-infra \
      -p "${LLAMA_PORT}:8001" \
      -v "${MODELS_DIR}:/models:ro" \
      "${LLAMA_IMAGE}" \
      --model /models/gemma-4-26B-A4B-it-UD-IQ4_NL.gguf \
      --alias gemma-4-26b-a4b \
      --port 8001 \
      --host 0.0.0.0 \
      --jinja \
      --flash-attn on \
      --ctx-size 262144 \
      --cache-type-k q8_0 \
      --cache-type-v q8_0 \
      --batch-size 512 \
      --ubatch-size 512 \
      --cont-batching \
      --parallel 4 \
      --metrics
    ;;

  text)
    echo "Starting Gemma-4-26B (text-only + thinking)..."
    docker run -d --name orchestrator-llama \
      --gpus all \
      --network shared-infra \
      -p "${LLAMA_PORT}:8001" \
      -v "${MODELS_DIR}:/models:ro" \
      "${LLAMA_IMAGE}" \
      --model /models/gemma-4-26B-A4B-it-UD-IQ4_NL.gguf \
      --alias gemma-4-26b-a4b \
      --port 8001 \
      --host 0.0.0.0 \
      --jinja \
      --flash-attn on \
      --ctx-size 262144 \
      --cache-type-k q8_0 \
      --cache-type-v q8_0 \
      --batch-size 512 \
      --ubatch-size 512 \
      --cont-batching \
      --parallel 2 \
      --metrics \
      --chat-template-kwargs '{"enable_thinking":true}'
    ;;

  fast)
    echo "Starting Gemma-4-26B vision (fast — no thinking)..."
    docker run -d --name orchestrator-llama \
      --gpus all \
      --network shared-infra \
      -p "${LLAMA_PORT}:8001" \
      -v "${MODELS_DIR}:/models:ro" \
      -v "${VISION_DIR}:/vision:ro" \
      "${LLAMA_IMAGE}" \
      --model /models/gemma-4-26B-A4B-it-UD-IQ4_NL.gguf \
      --mmproj /vision/gemma-4-26B-A4B-it-mmproj-F16.gguf \
      --alias gemma-4-26b-a4b \
      --port 8001 \
      --host 0.0.0.0 \
      --jinja \
      --flash-attn on \
      --ctx-size 262144 \
      --cache-ram 2048 \
      --cache-type-k q8_0 \
      --cache-type-v q8_0 \
      --batch-size 512 \
      --ubatch-size 512 \
      --cont-batching \
      --parallel 2 \
      --metrics \
      --chat-template-kwargs '{"enable_thinking":false}'
    ;;

  *)
    echo "Starting Gemma-4-26B vision (default — thinking enabled)..."
    docker run -d --name orchestrator-llama \
      --gpus all \
      --network shared-infra \
      -p "${LLAMA_PORT}:8001" \
      -v "${MODELS_DIR}:/models:ro" \
      -v "${VISION_DIR}:/vision:ro" \
      "${LLAMA_IMAGE}" \
      --model /models/gemma-4-26B-A4B-it-UD-IQ4_NL.gguf \
      --mmproj /vision/gemma-4-26B-A4B-it-mmproj-F16.gguf \
      --alias gemma-4-26b-a4b \
      --port 8001 \
      --host 0.0.0.0 \
      --jinja \
      --flash-attn on \
      --ctx-size 262144 \
      --cache-ram 2048 \
      --cache-type-k q8_0 \
      --cache-type-v q8_0 \
      --batch-size 512 \
      --ubatch-size 512 \
      --cont-batching \
      --parallel 2 \
      --metrics \
      --chat-template-kwargs '{"enable_thinking":true}'
    ;;
esac

echo "Waiting for model to load..."
for i in $(seq 1 90); do
  if curl -sf "http://localhost:${LLAMA_PORT}/health" > /dev/null 2>&1; then
    echo "Model server ready on port ${LLAMA_PORT}"
    exit 0
  fi
  sleep 2
done
echo "ERROR: Model server did not start within 180s"
docker logs orchestrator-llama 2>&1 | tail -5
exit 1
