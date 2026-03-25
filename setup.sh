#!/bin/bash
# Setup script for auto-developer-orchestrator
# Connects this project to shared-docker-infra

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SHARED_INFRA_DIR="${HOME}/Documents/programs/shared-docker-infra"

echo "============================================"
echo "Auto-Developer Orchestrator Setup"
echo "============================================"
echo ""

# Check if shared-docker-infra exists
if [ ! -d "$SHARED_INFRA_DIR" ]; then
    echo "❌ shared-docker-infra not found at: $SHARED_INFRA_DIR"
    echo ""
    echo "Please clone or navigate to the shared-docker-infra project first:"
    echo "  cd ~/Documents/programs"
    echo "  git clone <shared-docker-infra-repo>"
    exit 1
fi

echo "✓ Found shared-docker-infra at: $SHARED_INFRA_DIR"
echo ""

# Check if shared-infra network exists
if docker network ls | grep -q "shared-infra"; then
    echo "✓ Docker network 'shared-infra' exists"
else
    echo "⚠ Docker network 'shared-infra' not found"
    echo ""
    echo "Starting shared-docker-infra core services..."
    cd "$SHARED_INFRA_DIR"
    
    if command -v task &> /dev/null; then
        task core:up
    else
        docker compose up -d
    fi
    
    # Wait for network to be created
    echo "Waiting for network to be ready..."
    sleep 5
fi

# Check if Traefik is running
if docker ps | grep -q "traefik"; then
    echo "✓ Traefik is running"
else
    echo "⚠ Traefik is not running"
    echo ""
    echo "Please start shared-docker-infra first:"
    echo "  cd $SHARED_INFRA_DIR"
    echo "  task core:up"
    exit 1
fi

# Check if LiteLLM is running
if docker ps | grep -q "litellm"; then
    echo "✓ LiteLLM is running"
else
    echo "⚠ LiteLLM is not running"
    echo "  You can start it with: task up-litellm"
fi

# Check if Langfuse is running
if docker ps | grep -q "langfuse"; then
    echo "✓ Langfuse is running"
else
    echo "⚠ Langfuse is not running"
    echo "  You can start it with: task up-langfuse"
fi

# Setup .env file
cd "$SCRIPT_DIR"
if [ ! -f ".env" ]; then
    echo ""
    echo "Creating .env file from .env.example..."
    cp .env.example .env
    echo "✓ Created .env file"
    echo ""
    echo "⚠ Please edit .env and configure the following:"
    echo "  - Your API keys (JULES, OpenAI, Gemini, Claude, GitHub)"
    echo "  - Langfuse credentials (get from http://langfuse.local)"
    echo ""
else
    echo "✓ .env file already exists"
fi

echo ""
echo "============================================"
echo "Setup Complete!"
echo "============================================"
echo ""
echo "Next steps:"
echo "  1. Edit .env and add your API keys"
echo "  2. Get Langfuse credentials from http://langfuse.local"
echo "  3. Start the orchestrator:"
echo "     make dev-up    # Development"
echo "     make prod-up   # Production"
echo ""
echo "Access the dashboard at: http://orchestrator.local"
echo "============================================"
