#!/bin/bash
# setup.sh — Auto-Developer Orchestrator
# Run after git clone to install everything and get ready to go.
#
# Usage:
#   ./setup.sh           # full install
#   ./setup.sh --check   # just verify what's missing

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

CHECK_ONLY=false
[[ "${1:-}" == "--check" ]] && CHECK_ONLY=true

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
DIM='\033[2m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}OK${NC} $1"; }
warn() { echo -e "  ${YELLOW}!!${NC} $1"; }
fail() { echo -e "  ${RED}FAIL${NC} $1"; }
step() { echo -e "\n${DIM}── $1 ──${NC}"; }

ERRORS=0

need() {
    if ! command -v "$1" &>/dev/null; then
        fail "$1 not found — $2"
        ((ERRORS++))
        return 1
    fi
    return 0
}

# ────────────────────────────────────────────────────
echo ""
echo "  Auto-Developer Orchestrator — Setup"
echo ""

# ── 1. System dependencies ──────────────────────────
step "Checking system tools"

if need go "Install from https://go.dev/dl/ or: sudo snap install go --classic"; then
    ok "go $(go version | awk '{print $3}')"
fi

if need node "Install from https://nodejs.org/ or: sudo snap install node --classic"; then
    ok "node $(node --version)"
fi

if need bun "Install: curl -fsSL https://bun.sh/install | bash"; then
    ok "bun $(bun --version)"
fi

if need docker "Install: https://docs.docker.com/engine/install/"; then
    ok "docker $(docker --version | awk '{print $3}' | tr -d ',')"
fi

if need task "Install: https://taskfile.dev/install/ or: go install github.com/go-task/task/v3/cmd/task@latest"; then
    ok "task $(task --version 2>/dev/null | head -1 || echo '(installed)')"
fi

if need jq "Install: sudo apt install jq"; then
    ok "jq"
fi

if need python3 "Install: sudo apt install python3 python3-venv"; then
    ok "python3 $(python3 --version)"
fi

if need uv "Install: curl -LsSf https://astral.sh/uv/install.sh | sh"; then
    ok "uv $(uv --version)"
fi

if $CHECK_ONLY; then
    echo ""
    if [ "$ERRORS" -gt 0 ]; then
        echo -e "  ${RED}$ERRORS missing dependency(ies)${NC}"
        exit 1
    fi
    echo -e "  ${GREEN}All system dependencies present${NC}"
    exit 0
fi

if [ "$ERRORS" -gt 0 ]; then
    echo ""
    echo -e "  ${RED}Fix the $ERRORS missing dependency(ies) above, then re-run ./setup.sh${NC}"
    exit 1
fi

# ── 2. Shared infrastructure ────────────────────────
step "Checking shared-docker-infra"

SHARED_INFRA_DIR="${HOME}/Documents/programs/shared-docker-infra"
if [ -d "$SHARED_INFRA_DIR" ]; then
    ok "shared-docker-infra at $SHARED_INFRA_DIR"
else
    warn "shared-docker-infra not found at $SHARED_INFRA_DIR"
    echo "       Some features (Traefik, LiteLLM, model files) need it."
    echo "       Clone it to $SHARED_INFRA_DIR if you need them."
fi

if docker network ls 2>/dev/null | grep -q "shared-infra"; then
    ok "Docker network 'shared-infra' exists"
else
    warn "Docker network 'shared-infra' not found"
    if [ -d "$SHARED_INFRA_DIR" ]; then
        echo "       Creating it..."
        docker network create shared-infra 2>/dev/null || true
        ok "shared-infra network created"
    fi
fi

# ── 3. Node.js dependencies (frontend) ──────────────
step "Installing frontend dependencies"

if [ ! -d "node_modules" ]; then
    npm install --no-audit --no-fund
    ok "npm packages installed"
else
    ok "node_modules exists (run 'npm install' to update)"
fi

# ── 4. TUI dependencies (bun) ───────────────────────
step "Installing TUI dependencies"

if [ -d "ts-tui-pi" ]; then
    cd ts-tui-pi
    if [ ! -d "node_modules" ]; then
        bun install --frozen-lockfile 2>/dev/null || bun install
        ok "bun packages installed"
    else
        ok "node_modules exists (run 'bun install' to update)"
    fi
    cd "$SCRIPT_DIR"
fi

# ── 5. Go backend ───────────────────────────────────
step "Setting up Go backend"

cd go-backend
go mod tidy 2>/dev/null || true
ok "go modules tidied"

echo "  Building server + CLI..."
go build -o server ./cmd/server/
go build -o orch ./cmd/cli/
ok "built server and orch binaries"
cd "$SCRIPT_DIR"

# ── 6. Python E2E test environment ──────────────────
step "Setting up Python E2E tests"

if [ -f "tests/python/pyproject.toml" ]; then
    cd tests/python
    if [ ! -d ".venv" ]; then
        uv venv .venv
        ok "created .venv"
    else
        ok ".venv exists"
    fi
    uv pip install -e ".[dev]" 2>/dev/null || uv pip install -e . 2>/dev/null || uv sync 2>/dev/null || true
    ok "Python test dependencies installed"
    cd "$SCRIPT_DIR"
fi

# ── 7. Sandbox Docker image ─────────────────────────
step "Building sandbox Docker image"

if [ -f "sandbox/Dockerfile" ]; then
    echo "  Building orchestrator-sandbox (may take a few minutes on first run)..."
    docker build -t orchestrator-sandbox sandbox/ --quiet 2>/dev/null || \
    docker build -t orchestrator-sandbox sandbox/ || \
    warn "Sandbox image build failed — build manually with: docker build -t orchestrator-sandbox sandbox/"
    ok "sandbox image ready"
fi

# ── 8. Environment file ─────────────────────────────
step "Setting up environment"

if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        cp .env.example .env
        ok ".env created from .env.example"
        warn "Edit .env and add your API keys"
    else
        warn "No .env.example found — create .env manually"
    fi
else
    ok ".env already exists"
fi

# ── 9. Data directories ─────────────────────────────
step "Creating data directories"

mkdir -p data
mkdir -p data/backups
mkdir -p "$HOME/.pi/agent/sessions"
ok "data/ and ~/.pi/agent/ ready"

# ── 10. Git hooks ───────────────────────────────────
step "Git hooks"

if [ -f "scripts/setup-hooks.sh" ]; then
    bash scripts/setup-hooks.sh 2>/dev/null || true
    ok "hooks configured"
else
    ok "no custom hooks"
fi

# ── Done ─────────────────────────────────────────────
echo ""
echo -e "  ${GREEN}Setup complete!${NC}"
echo ""
echo "  Quick start:"
echo "    task model          # start model server (needs GPU + model files)"
echo "    task dev            # start Go backend + Vite frontend"
echo "    task chat           # launch interactive TUI"
echo "    task --list         # show all available tasks"
echo ""
echo "  If model server is unavailable, the backend runs in sandbox-only mode"
echo "  and can use cloud providers (OpenRouter, Gemini) instead."
echo ""
