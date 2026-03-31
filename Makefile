# Auto-Developer Orchestrator - Unified Makefile
# ==================================================

# Default target
.PHONY: help
help:
	@echo "🚀 Auto-Developer Orchestrator - Development Interface"
	@echo ""
	@echo "Local Development (Native):"
	@echo "  make install         - Install dependencies (npm, go, python/uv)"
	@echo "  make dev             - Start all services locally (Vite + Go + Python)"
	@echo "  make dev-frontend    - Start Vite frontend only"
	@echo "  make dev-backend     - Start Go backend only"
	@echo "  make docs            - Start documentation site (Port 3001)"
	@echo ""
	@echo "Docker Development (Recommended):"
	@echo "  make up              - Start dev environment with hot reload"
	@echo "  make down            - Stop dev environment"
	@echo "  make restart         - Restart dev environment"
	@echo "  make logs            - Follow all logs"
	@echo ""
	@echo "Production:"
	@echo "  make prod-up         - Start production environment"
	@echo "  make prod-down       - Stop production environment"
	@echo ""
	@echo "Testing & Linting:"
	@echo "  make test            - Run JS, Go, and E2E tests"
	@echo "  make test-go         - Run Go backend tests"
	@echo "  make test-js         - Run frontend unit tests"
	@echo "  make test-e2e        - Run Playwright E2E tests"
	@echo "  make lint            - Run all linters"
	@echo ""
	@echo "Maintenance:"
	@echo "  make clean           - Remove build artifacts and docker resources"
	@echo "  make db-backup       - Backup SQLite database"
	@echo "  make db-restore      - Restore SQLite database"
	@echo "  make infra-check     - Check health of shared-docker-infra"

# ==============================================================================
# Variables
# ==============================================================================
COMPOSE_DEV = docker compose -f docker-compose.dev.yml
COMPOSE_PROD = docker compose -f docker-compose.yml
PYTHON_PORT = 8080
GO_PORT = 3847
JS_PORT = 5174

# ==============================================================================
# Installation
# ==============================================================================
.PHONY: install
install:
	@echo "📦 Installing frontend dependencies..."
	npm install --no-audit --no-fund
	@echo "📦 Syncing Python agent with uv..."
	cd python-agent && uv sync
	@echo "📦 Tidying Go modules..."
	cd go-backend && go mod tidy
	@echo "📦 Installing documentation dependencies..."
	cd docs && npm install --no-audit --no-fund
	@echo "✅ Installation complete"

# ==============================================================================
# Local Development
# ==============================================================================
.PHONY: dev dev-frontend dev-backend
dev:
	@echo "🚀 Starting all services locally..."
	@echo "1. Starting Python agent (Port $(PYTHON_PORT))..."
	@cd python-agent && (uv run uvicorn main:app --port $(PYTHON_PORT) &)
	@echo "2. Starting Go backend (Port $(GO_PORT))..."
	@cd go-backend && (go run cmd/server/main.go &)
	@echo "3. Starting Vite frontend (Port $(JS_PORT))..."
	@npm run dev -- --port $(JS_PORT)

dev-frontend:
	npm run dev -- --port $(JS_PORT)

dev-backend:
	cd go-backend && go run cmd/server/main.go

.PHONY: docs
docs:
	@echo "📖 Starting documentation site (Port 3001)..."
	@cd docs && npm run dev -- -p 3001

# ==============================================================================
# Docker Development
# ==============================================================================
.PHONY: up down restart logs
up:
	@echo "🚀 Starting development environment (Docker)..."
	$(COMPOSE_DEV) up -d --build
	@echo "✅ Services started. Access at http://orchestrator.local"

down:
	$(COMPOSE_DEV) down --remove-orphans

restart: down up

logs:
	$(COMPOSE_DEV) logs -f

# ==============================================================================
# Production
# ==============================================================================
.PHONY: prod-up prod-down
prod-up:
	@echo "🚀 Starting production environment..."
	$(COMPOSE_PROD) up -d --build
	@echo "✅ Production services started on http://orchestrator.local"

prod-down:
	$(COMPOSE_PROD) down

# ==============================================================================
# Testing
# ==============================================================================
.PHONY: test test-go test-js test-e2e
test: test-js test-go test-e2e

test-go:
	@echo "🧪 Running Go backend tests..."
	cd go-backend && go test -v ./...

test-js:
	@echo "🧪 Running frontend unit tests..."
	npm run test

test-e2e:
	@echo "🧪 Running Playwright E2E tests..."
	npm run test:playwright

# ==============================================================================
# Maintenance & Utilities
# ==============================================================================
.PHONY: clean lint db-backup db-restore infra-check

lint:
	@echo "🧹 Linting Go backend..."
	cd go-backend && go fmt ./...
	@echo "🧹 Linting frontend..."
	npm run lint

clean:
	@echo "🧹 Cleaning artifacts..."
	rm -rf dist go-backend/bin 
	$(COMPOSE_DEV) down -v --remove-orphans
	@echo "✅ Clean complete"

db-backup:
	@mkdir -p data/backups
	@cp data/orchestrator.db data/backups/orchestrator.db.backup.$$(date +%Y%m%d_%H%M%S)
	@echo "✅ Database backed up to data/backups/"

db-restore:
	@echo "Select a backup to restore:"
	@ls -1 data/backups/
	@read -p "Filename: " backup; \
	cp data/backups/$$backup data/orchestrator.db
	@echo "✅ Database restored"

infra-check:
	@echo "🔍 Checking shared infrastructure health..."
	@echo -n "Traefik:  " && curl -s http://traefik.local/ping > /dev/null && echo "✅ OK" || echo "❌ DOWN"
	@echo -n "LiteLLM:  " && curl -s http://litellm.local/health > /dev/null && echo "✅ OK" || echo "❌ DOWN (Optional)"
	@echo -n "Langfuse: " && curl -s http://langfuse.local/api/health > /dev/null && echo "✅ OK" || echo "❌ DOWN (Optional)"
