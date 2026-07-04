#!/usr/bin/env bash
# bootstrap-vision.sh — download Qwen3.5-2B-ONNX-OPT (fp16 vision) for pux.
#
# Downloads to <project>/.pux/models/Qwen3.5-2B-ONNX-OPT/ so the model is
# bind-mounted into the sandbox at /sandbox/workspace/.pux/models/... when the
# pux sandbox boots for that project. The describe_image tool looks for the
# model there.
#
# Applies the known patch_size bug fix: deletes the `"patch_size": 16` line
# from genai_config.json. Older onnxruntime-genai versions (≤1.4.3) trip a
# JSON parse error on this field and refuse to load the model.
#
# Usage:
#   scripts/bootstrap-vision.sh                # download to $PWD/.pux/models
#   scripts/bootstrap-vision.sh --project DIR  # explicit project root
#   scripts/bootstrap-vision.sh --check        # exit 0 if ready, 1 if not
#   scripts/bootstrap-vision.sh --force        # re-download even if present
#
# Requirements: curl, jq, python3 (with huggingface_hub preferred but optional).
# HuggingFace downloads are resume-friendly; partial downloads complete on re-run.
set -euo pipefail

MODEL_REPO="onnx-community/Qwen3.5-2B-ONNX-OPT"
MODEL_DIR_NAME="Qwen3.5-2B-ONNX-OPT"

# Required files for fp16 vision inference. If all present, model is ready.
REQUIRED_FILES=(
    "genai_config.json"
    "preprocessor_config.json"
    "model.onnx"
    "model.onnx.data"
    "vision_encoder_fp16.onnx"
    "vision_encoder_fp16.onnx.data"
)

# Parse args.
PROJECT="${PUX_PROJECT_PATH:-$PWD}"
CHECK_ONLY=0
FORCE=0
for arg in "$@"; do
    case "$arg" in
        --project) shift; PROJECT="${1:-$PROJECT}" ;;
        --check) CHECK_ONLY=1 ;;
        --force) FORCE=1 ;;
        -h|--help)
            grep '^#' "$0" | sed 's/^# //; s/^#//'
            exit 0
            ;;
        *)
            if [ -n "${1:-}" ] && [ -z "${PROJECT_OVERRIDE:-}" ]; then
                PROJECT="$arg"
                PROJECT_OVERRIDE=1
            fi
            ;;
    esac
    shift || true
done

MODEL_DIR="$(cd "$PROJECT" 2>/dev/null && pwd)/.pux/models/$MODEL_DIR_NAME"
mkdir -p "$MODEL_DIR"

log() { printf '[bootstrap-vision] %s\n' "$*" >&2; }
die() { printf '[bootstrap-vision] ERROR: %s\n' "$*" >&2; exit 1; }

# ── 1. Check dependencies ────────────────────────────────────────────────
command -v curl >/dev/null || die "curl is required"
command -v jq   >/dev/null || die "jq is required (apt install jq / brew install jq)"

# ── 2. Check mode ─────────────────────────────────────────────────────────
check_ready() {
    for f in "${REQUIRED_FILES[@]}"; do
        [ -f "$MODEL_DIR/$f" ] || return 1
    done
    # patch_size fix must also be applied (or absent in the original).
    if grep -q '"patch_size"[[:space:]]*:[[:space:]]*16' "$MODEL_DIR/genai_config.json" 2>/dev/null; then
        return 1
    fi
    return 0
}

if [ "$CHECK_ONLY" -eq 1 ]; then
    if check_ready; then
        log "ready: $MODEL_DIR"
        exit 0
    fi
    log "not ready: $MODEL_DIR"
    exit 1
fi

if check_ready && [ "$FORCE" -ne 1 ]; then
    log "already ready (use --force to re-download): $MODEL_DIR"
    exit 0
fi

# ── 3. Download ───────────────────────────────────────────────────────────
# Use huggingface-cli if available (resume support, parallel downloads);
# fall back to direct curl per-file.
HF_BASE="https://huggingface.co/${MODEL_REPO}/resolve/main"

download_file() {
    local fname="$1"
    local url="$HF_BASE/$fname"
    local dest="$MODEL_DIR/$fname"
    log "downloading $fname"
    # -L: follow redirects (HF redirects to CDN). -C -: resume. --fail: error on 404.
    if ! curl -L --fail -C - -o "$dest" "$url"; then
        die "download failed: $fname"
    fi
}

if command -v huggingface-cli >/dev/null 2>&1; then
    log "using huggingface-cli (preferred)"
    # Download the entire repo, but only the fp16 + config files we need.
    # --include globs match the relevant subset; --local-dir uses the canonical layout.
    if ! huggingface-cli download "$MODEL_REPO" \
        --local-dir "$MODEL_DIR" \
        --local-dir-use-symlinks False \
        --include "genai_config.json" \
        --include "preprocessor_config.json" \
        --include "model.onnx*" \
        --include "vision_encoder_fp16.onnx*" \
        --include "tokenizer*" \
        --include "special_tokens_map.json" \
        --include "vocab.json" \
        --include "merges.txt" \
        --include "configuration.json" 2>&1 | sed 's/^/[hf] /' >&2; then
        log "huggingface-cli failed, falling back to curl"
        for f in "${REQUIRED_FILES[@]}"; do
            download_file "$f"
        done
    fi
else
    log "huggingface-cli not found, using curl"
    for f in "${REQUIRED_FILES[@]}"; do
        download_file "$f"
    done
fi

# ── 4. Apply patch_size fix ───────────────────────────────────────────────
# https://github.com/onnx-community/qwen3.5-onnx issue: older onnxruntime-genai
# JSON parser rejects "patch_size": 16 under model.vision. Delete the line.
GENAI_CFG="$MODEL_DIR/genai_config.json"
if [ -f "$GENAI_CFG" ] && grep -q '"patch_size"' "$GENAI_CFG"; then
    log "applying patch_size fix to genai_config.json"
    # jq walk: delete .model.vision.patch_size if present, leave everything else.
    tmp="$(mktemp)"
    jq 'del(.model.vision.patch_size)' "$GENAI_CFG" > "$tmp" && mv "$tmp" "$GENAI_CFG"
fi

# ── 5. Verify ─────────────────────────────────────────────────────────────
if ! check_ready; then
    log "post-download check failed; missing files:"
    for f in "${REQUIRED_FILES[@]}"; do
        [ -f "$MODEL_DIR/$f" ] || log "  - $f"
    done
    die "model incomplete"
fi

# Quick sanity: fp16 vision encoder weights should be > 100MB.
SIZE_BIN=$(stat -c '%s' "$MODEL_DIR/vision_encoder_fp16.onnx" 2>/dev/null || stat -f '%z' "$MODEL_DIR/vision_encoder_fp16.onnx")
if [ "$SIZE_BIN" -lt 104857600 ]; then
    die "vision_encoder_fp16.onnx is only ${SIZE_BIN} bytes — expected >100MB. Partial download?"
fi

log "ready: $MODEL_DIR"
log "next: boot pux (pux sandbox start) — describe_image picks up the bind-mounted model automatically (no image rebuild needed)."
