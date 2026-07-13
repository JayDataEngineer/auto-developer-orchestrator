# auto-developer-orchestrator — developer commands.
#
# First time setup:
#   git clone --recurse-submodules <repo>
#   make sandbox           # build the pux-sandbox Docker image
#   make infra             # start SurrealDB + media-mcp (host-side services)
#
# After that, any org works:
#   uv run pux direct --org deep-research-engine --task "..."
#
# GPU media-mcp (optional, faster ASR/vision):
#   MEDIA_DEVICE=cuda TORCH_VARIANT=cu124 make infra
#
# Remote infra (NOT managed here — game-studio only, bring your own GPU box):
#   Ray cluster on Tailscale — LLM, TTS, 3D, music, ComfyUI.

.PHONY: help submodules infra infra-core infra-embeddings \
        infra-status infra-down infra-destroy infra-logs \
        sandbox sandbox-shell test test-quick test-live clean

INFRA_COMPOSE := docker compose -f docker-compose.infra.yml

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ── Submodules ──────────────────────────────────────────────────────────────

submodules: ## Init + update git submodules (pux-harness, infra/media-mcp)
	git submodule update --init --recursive

# ── Host-side infrastructure ────────────────────────────────────────────────

infra: ## Start SurrealDB + media-mcp + TEI embeddings (everything DRE needs)
	$(INFRA_COMPOSE) up -d surrealdb media-mcp tei
	@echo ""
	@echo "Waiting for SurrealDB to be healthy..."
	@timeout 30 sh -c 'until curl -sf http://localhost:8000/health >/dev/null 2>&1; do sleep 1; done' \
		&& echo "SurrealDB healthy" || echo "WARNING: SurrealDB not healthy — check: make infra-logs"
	@echo "SurrealDB at http://localhost:8000 (root:root, MCP at /mcp)"
	@echo "media-mcp at http://localhost:8101"
	@echo "TEI embeddings at http://localhost:8080 (harrier-oss-v1-0.6b)"
	@echo ""
	@echo "Infra is up. Run any org: uv run pux direct --org deep-research-engine --task '...'"

infra-core: ## Start SurrealDB only (lighter — skip media-mcp + TEI model load)
	$(INFRA_COMPOSE) up -d surrealdb
	@echo ""
	@echo "Waiting for SurrealDB to be healthy..."
	@timeout 30 sh -c 'until curl -sf http://localhost:8000/health >/dev/null 2>&1; do sleep 1; done' \
		&& echo "SurrealDB healthy" || echo "WARNING: SurrealDB not healthy — check: make infra-logs"
	@echo "SurrealDB at http://localhost:8000 (root:root)"

infra-embeddings: ## Start TEI embeddings only (harrier-oss-v1-0.6b)
	$(INFRA_COMPOSE) up -d tei
	@echo ""
	@echo "Waiting for TEI to be healthy..."
	@timeout 120 sh -c 'until curl -sf http://localhost:8080/health >/dev/null 2>&1; do sleep 2; done' \
		&& echo "TEI healthy (harrier-oss-v1-0.6b)" || echo "WARNING: TEI not healthy — check: make infra-logs"

infra-status: ## Show infra container status
	$(INFRA_COMPOSE) ps

infra-logs: ## Tail infra logs
	$(INFRA_COMPOSE) logs -f --tail=50

infra-down: ## Stop infra (data volumes preserved)
	$(INFRA_COMPOSE) down

infra-destroy: ## Stop infra AND wipe data volumes (irreversible)
	$(INFRA_COMPOSE) down -v
	@echo "Data volumes wiped. Next 'make infra' starts fresh."

# ── Sandbox image ───────────────────────────────────────────────────────────

sandbox: ## Build the pux-sandbox Docker image
	docker build -t pux-sandbox:latest sandbox/

sandbox-shell: ## Drop into a shell in the sandbox container
	docker exec -it orchestrator-sandbox-mcp-default bash || \
		echo "Sandbox not running. Start it with: uv run pux direct --org general --task 'hello'"

# ── Tests ───────────────────────────────────────────────────────────────────

test: ## Run the full test suite (no live/E2E tests)
	uv run pytest -q

test-quick: ## Run the fastest unit tests only
	uv run pytest tests/contract/ tests/sandbox/ -q

test-live: ## Run live E2E tests (needs PUX_E2E=1 + .env with API key)
	PUX_E2E=1 uv run pytest tests/integration/ -q

# ── Misc ────────────────────────────────────────────────────────────────────

clean: ## Remove Python caches + .pyc files
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -name "*.pyc" -delete 2>/dev/null || true
	rm -rf .pytest_cache .mypy_cache 2>/dev/null || true
