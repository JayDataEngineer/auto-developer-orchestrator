# Auto-Developer Orchestrator - Go Backend Makefile
# ==================================================

.PHONY: help dev-up dev-down prod-up prod-down logs clean test lint build

# Default target
help:
	@echo "Auto-Developer Orchestrator - Go Backend"
	@echo ""
	@echo "Development:"
	@echo "  make dev-up          - Start development environment with hot reload"
	@echo "  make dev-down        - Stop development environment"
	@echo "  make dev-restart     - Restart development environment"
	@echo "  make logs            - View logs (follow mode)"
	@echo "  make logs-go         - View Go backend logs only"
	@echo "  make logs-python     - View Python agent logs only"
	@echo ""
	@echo "Production:"
	@echo "  make prod-up         - Start production environment"
	@echo "  make prod-down       - Stop production environment"
	@echo "  make prod-restart    - Restart production environment"
	@echo ""
	@echo "Build & Test:"
	@echo "  make build           - Build Go binary"
	@echo "  make test            - Run Go tests"
	@echo "  make lint            - Run Go linter"
	@echo "  make clean           - Clean build artifacts"
	@echo ""
	@echo "Database:"
	@echo "  make migrate         - Run database migrations"
	@echo "  make db-backup       - Backup SQLite database"
	@echo "  make db-restore      - Restore SQLite database"

# ===========================================
# Development
# ===========================================

dev-up:
	@echo "Starting development environment..."
	docker-compose -f docker-compose.dev.go.yml up -d
	@echo "Development environment started!"
	@echo ""
	@echo "Services:"
	@echo "  - Go Backend:    http://localhost:3847"
	@echo "  - Python Agent:  http://localhost:8080"
	@echo "  - Frontend:      http://localhost:5173"
	@echo ""
	@echo "View logs: make logs"

dev-down:
	@echo "Stopping development environment..."
	docker-compose -f docker-compose.dev.go.yml down

dev-restart: dev-down dev-up

logs:
	docker-compose -f docker-compose.dev.go.yml logs -f

logs-go:
	docker-compose -f docker-compose.dev.go.yml logs -f go-backend

logs-python:
	docker-compose -f docker-compose.dev.go.yml logs -f python-agent

logs-frontend:
	docker-compose -f docker-compose.dev.go.yml logs -f frontend

# ===========================================
# Production
# ===========================================

prod-up:
	@echo "Starting production environment..."
	docker-compose -f docker-compose.go.yml up -d
	@echo "Production environment started!"
	@echo ""
	@echo "Services:"
	@echo "  - Nginx:         http://localhost:80"
	@echo "  - Go Backend:    http://localhost:3847"
	@echo "  - Python Agent:  http://localhost:8080"

prod-down:
	@echo "Stopping production environment..."
	docker-compose -f docker-compose.go.yml down

prod-restart: prod-down prod-up

# ===========================================
# Build & Test
# ===========================================

build:
	@echo "Building Go backend..."
	cd go-backend && go build -o bin/server ./cmd/server
	@echo "Build complete!"

test:
	@echo "Running Go tests..."
	cd go-backend && go test -v ./...

test-coverage:
	@echo "Running Go tests with coverage..."
	cd go-backend && go test -v -coverprofile=coverage.out ./...
	go tool cover -html=go-backend/coverage.out -o go-backend/coverage.html

lint:
	@echo "Running Go linter..."
	cd go-backend && golangci-lint run

clean:
	@echo "Cleaning build artifacts..."
	rm -rf go-backend/bin
	rm -rf go-backend/coverage.out
	rm -rf go-backend/coverage.html
	rm -f data/orchestrator.db
	@echo "Clean complete!"

# ===========================================
# Database
# ===========================================

migrate:
	@echo "Running database migrations..."
	@echo "Migrations are auto-run on startup"

db-backup:
	@echo "Backing up database..."
	cp data/orchestrator.db data/orchestrator.db.backup.$$(date +%Y%m%d_%H%M%S)
	@echo "Backup complete!"

db-restore:
	@echo "Restoring database from backup..."
	@ls -lt data/*.backup.* | head -5
	@read -p "Enter backup file to restore: " backup; \
	cp data/$$backup data/orchestrator.db
	@echo "Restore complete!"

# ===========================================
# Go Module Management
# ===========================================

deps:
	@echo "Updating Go dependencies..."
	cd go-backend && go get -u ./...
	cd go-backend && go mod tidy

deps-vendor:
	@echo "Vendoring Go dependencies..."
	cd go-backend && go mod vendor

# ===========================================
# Code Generation
# ===========================================

generate:
	@echo "Generating code from templates..."
	cd go-backend && go generate ./...

# ===========================================
# Docker Management
# ===========================================

docker-clean:
	@echo "Cleaning Docker resources..."
	docker system prune -af
	docker builder prune -af
	@echo "Docker clean complete!"

docker-rebuild:
	@echo "Rebuilding Docker images without cache..."
	docker-compose -f docker-compose.go.yml build --no-cache

# ===========================================
# SSH Keys for Git
# ===========================================

ssh-test:
	@echo "Testing SSH connection to GitHub..."
	docker-compose -f docker-compose.go.yml run --rm go-backend ssh -T git@github.com

# ===========================================
# Health Checks
# ===========================================

health:
	@echo "Checking service health..."
	@echo -n "Go Backend:    "
	@curl -s -o /dev/null -w "%{http_code}" http://localhost:3847/api/health || echo "DOWN"
	@echo ""
	@echo -n "Python Agent:  "
	@curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health || echo "DOWN"
	@echo ""

# ===========================================
# Project Management
# ===========================================

project-clone:
	@read -p "Enter repository URL: " url; \
	curl -X POST http://localhost:3847/api/clone \
		-H "Content-Type: application/json" \
		-d "{\"url\": \"$$url\"}"

project-list:
	@echo "Listing projects..."
	curl -s http://localhost:3847/api/projects | jq .

# ===========================================
# Quick Commands
# ===========================================

# Start dev environment and view logs
dev: dev-up logs

# Quick rebuild and restart
rebuild: build prod-restart

# View real-time logs with filtering
tail-logs:
	docker-compose -f docker-compose.dev.go.yml logs -f --tail=100
