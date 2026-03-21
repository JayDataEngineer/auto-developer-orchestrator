# Auto-Developer Orchestrator Makefile
# Full Developer Experience with Hot Reload

.PHONY: help dev-up dev-down dev-restart clean install build prod-up prod-down logs shell status restart-app test test-e2e

# Default target
help:
	@echo "Auto-Developer Orchestrator - Development Commands"
	@echo ""
	@echo "Development:"
	@echo "  dev-up          Start development environment with hot reload"
	@echo "  dev-down        Stop development environment"
	@echo "  dev-restart     Restart development environment"
	@echo "  logs            View live logs"
	@echo ""
	@echo "Testing:"
	@echo "  test            Run unit tests"
	@echo "  test-unit       Run unit tests"
	@echo "  test-integration Run integration tests"
	@echo "  test-e2e        Run E2E tests (requires server running)"
	@echo "  test-all        Run unit, integration, and E2E tests"
	@echo ""
	@echo "Production:"
	@echo "  prod-up         Start production environment"
	@echo "  prod-down       Stop production environment"
	@echo ""
	@echo "Utilities:"
	@echo "  setup           Install dependencies and setup git hooks"
	@echo "  install         Install Node.js dependencies locally"
	@echo "  build           Build for production"
	@echo "  clean           Remove containers and volumes"
	@echo "  shell           Open shell in app container"
	@echo "  status          Check service status"
	@echo ""

# Docker Compose command (v2 syntax)
COMPOSE = docker compose -f docker-compose.dev.yml

# Development Environment
dev-up:
	@echo "🚀 Starting development environment with hot reload..."
	$(COMPOSE) up -d --build
	@echo "✅ Services started!"
	@echo ""
	@echo "📍 Access points:"
	@echo "   - App Server:   http://localhost:3847"
	@echo ""
	@echo "📝 View logs: make logs"
	@echo "🛑 Stop:        make dev-down"

dev-down:
	@echo "🛑 Stopping development environment..."
	$(COMPOSE) down
	@echo "✅ Services stopped"

dev-restart: dev-down dev-up
	@echo "🔄 Development environment restarted"

# Production Environment
prod-up:
	@echo "🚀 Starting production environment..."
	docker compose up -d --build
	@echo "✅ Production services started on http://localhost:3847"

prod-down:
	@echo "🛑 Stopping production environment..."
	docker compose down

# Logs
logs:
	$(COMPOSE) logs -f

# Local Development (without Docker)
install:
	@echo "📦 Installing dependencies..."
	npm install --legacy-peer-deps

setup: install
	@echo "🔧 Setting up project and git hooks..."
	chmod +x scripts/setup-hooks.sh
	./scripts/setup-hooks.sh

build:
	@echo "🔨 Building for production..."
	npm run build

# Testing
test:
	@echo "🧪 Running all tests..."
	npm run test

test-unit:
	@echo "🧪 Running unit tests..."
	npx vitest run tests/unit

test-integration:
	@echo "🧪 Running integration tests..."
	@echo "⚠️  Make sure server is running: make dev-up"
	npx vitest run tests/integration

test-e2e:
	@echo "🧪 Running E2E tests..."
	@echo "⚠️  Make sure server is running: make dev-up"
	npm run test:e2e

test-all:
	@echo "🧪 Running unit, integration, and E2E tests..."
	@echo "⚠️  Make sure server is running: make dev-up"
	npx vitest run tests/unit && npx vitest run tests/integration && npm run test:e2e

# Container Utilities
clean:
	@echo "🧹 Cleaning containers and volumes..."
	$(COMPOSE) down -v --remove-orphans
	docker compose down -v --remove-orphans 2>/dev/null || true
	@echo "✅ Clean complete"

shell:
	@echo "🔌 Opening shell in app container..."
	$(COMPOSE) exec app sh

# Status check
status:
	@echo "📊 Service Status:"
	$(COMPOSE) ps

# Restart service
restart-app:
	@echo "🔄 Restarting app service..."
	$(COMPOSE) restart app
