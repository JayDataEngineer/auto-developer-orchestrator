# auto-developer-orchestrator — developer commands.
#
# The repo IS a dcode workspace: profiles/ projects onto dcode's native surface
# via src/ (the compiler + src/run.py). First time setup:
#   uv sync              # deepagents 0.7.5 + deepagents-code 0.1.55 (dcode's own pins)
#   make infra           # start SurrealDB + media-mcp (host-side services)
#
# After that, any org works:
#   uv run python src/run.py --org deep-research-engine --dry-run
#   uv run pux compile --org coder --out /tmp/staging
#
# GPU media-mcp (optional, faster ASR/vision):
#   MEDIA_DEVICE=cuda TORCH_VARIANT=cu124 make infra
#
# Remote infra (NOT managed here — game-studio only, bring your own GPU box):
#   Ray cluster on Tailscale — LLM, TTS, 3D, music, ComfyUI.

.PHONY: help infra infra-core infra-embeddings \
        infra-status infra-down infra-destroy infra-logs \
        test clean

INFRA_COMPOSE := docker compose -f docker-compose.infra.yml

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ── Host-side infrastructure ────────────────────────────────────────────────

infra: ## Start SurrealDB + media-mcp (host-side services)
	$(INFRA_COMPOSE) up -d surrealdb media-mcp
	@echo ""
	@echo "Waiting for SurrealDB to be healthy..."
	@timeout 30 sh -c 'until curl -sf http://localhost:8000/health >/dev/null 2>&1; do sleep 1; done' \
		&& echo "SurrealDB healthy" || echo "WARNING: SurrealDB not healthy — check: make infra-logs"
	@echo "SurrealDB at http://localhost:8000 (root:root, MCP at /mcp)"
	@echo "media-mcp at http://localhost:8101"
	@echo ""
	@echo "Infra is up. Run any org: uv run python src/run.py --org deep-research-engine --dry-run"

infra-core: ## Start SurrealDB only (lighter — skip media-mcp)
	$(INFRA_COMPOSE) up -d surrealdb
	@echo ""
	@echo "Waiting for SurrealDB to be healthy..."
	@timeout 30 sh -c 'until curl -sf http://localhost:8000/health >/dev/null 2>&1; do sleep 1; done' \
		&& echo "SurrealDB healthy" || echo "WARNING: SurrealDB not healthy — check: make infra-logs"
	@echo "SurrealDB at http://localhost:8000 (root:root)"

infra-status: ## Show infra container status
	$(INFRA_COMPOSE) ps

infra-logs: ## Tail infra logs
	$(INFRA_COMPOSE) logs -f --tail=50

infra-down: ## Stop infra (data volumes preserved)
	$(INFRA_COMPOSE) down

infra-destroy: ## Stop infra AND wipe data volumes (irreversible)
	$(INFRA_COMPOSE) down -v
	@echo "Data volumes wiped. Next 'make infra' starts fresh."

# ── Tests ───────────────────────────────────────────────────────────────────

test: ## Run the full test suite (no live/E2E tests)
	uv run pytest -q

# ── Misc ────────────────────────────────────────────────────────────────────

clean: ## Remove Python caches + .pyc files
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -name "*.pyc" -delete 2>/dev/null || true
	rm -rf .pytest_cache .mypy_cache 2>/dev/null || true
