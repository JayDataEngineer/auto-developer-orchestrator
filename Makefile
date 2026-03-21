# Auto-Developer Orchestrator Makefile
# Full Developer Experience with Hot Reload

.PHONY: help dev-up dev-down dev-restart clean install build prod-up prod-down logs shell

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
	@echo "Production:"
	@echo "  prod-up         Start production environment"
	@echo "  prod-down       Stop production environment"
	@echo ""
	@echo "Utilities:"
	@echo "  install         Install Node.js dependencies locally"
	@echo "  build           Build for production"
	@echo "  clean           Remove containers and volumes"
	@echo "  shell           Open shell in app container"
	@echo ""

# Development Environment
dev-up:
	@echo "🚀 Starting development environment with hot reload..."
	docker-compose -f docker-compose.dev.yml up -d --build
	@echo "✅ Services started!"
	@echo ""
	@echo "📍 Access points:"
	@echo "   - App Server:   http://localhost:3847"
	@echo ""
	@echo "📝 View logs: make logs"
	@echo "🛑 Stop:        make dev-down"

dev-down:
	@echo "🛑 Stopping development environment..."
	docker-compose -f docker-compose.dev.yml down
	@echo "✅ Services stopped"

dev-restart: dev-down dev-up
	@echo "🔄 Development environment restarted"

# Production Environment
prod-up:
	@echo "🚀 Starting production environment..."
	docker-compose up -d --build
	@echo "✅ Production services started on http://localhost:3000"

prod-down:
	@echo "🛑 Stopping production environment..."
	docker-compose down

# Logs
logs:
	docker-compose -f docker-compose.dev.yml logs -f

# Local Development (without Docker)
install:
	@echo "📦 Installing dependencies..."
	npm install

build:
	@echo "🔨 Building for production..."
	npm run build

# Container Utilities
clean:
	@echo "🧹 Cleaning containers and volumes..."
	docker-compose -f docker-compose.dev.yml down -v --remove-orphans
	docker-compose down -v --remove-orphans 2>/dev/null || true
	@echo "✅ Clean complete"

shell:
	@echo "🔌 Opening shell in app container..."
	docker-compose -f docker-compose.dev.yml exec app sh

# Status check
status:
	@echo "📊 Service Status:"
	docker-compose -f docker-compose.dev.yml ps

# Restart service
restart-app:
	@echo "🔄 Restarting app service..."
	docker-compose -f docker-compose.dev.yml restart app
