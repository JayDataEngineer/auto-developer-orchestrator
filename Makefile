# auto-developer-orchestrator — a plain dcode workspace.
#
# The runtime IS dcode: run `dcode` in the repo. Host-side services:
#   make infra           # SurrealDB (:8000) + media-mcp (:8101)
#   make infra-core      # SurrealDB only (lighter)
#
# GPU media-mcp (optional, faster ASR/vision):
#   MEDIA_DEVICE=cuda TORCH_VARIANT=cu124 make infra
#
# Remote infra (NOT managed here — bring your own GPU box):
#   Ray cluster on Tailscale — LLM, TTS, 3D, music, ComfyUI.

.PHONY: help infra infra-core infra-nitter infra-status infra-down infra-destroy infra-logs hooks clean sandbox-config sandbox sandbox-status sandbox-stop

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
	@echo "Infra is up. Run: dcode"

infra-core: ## Start SurrealDB only (lighter — skip media-mcp)
	$(INFRA_COMPOSE) up -d surrealdb
	@echo ""
	@echo "Waiting for SurrealDB to be healthy..."
	@timeout 30 sh -c 'until curl -sf http://localhost:8000/health >/dev/null 2>&1; do sleep 1; done' \
		&& echo "SurrealDB healthy" || echo "WARNING: SurrealDB not healthy — check: make infra-logs"
	@echo "SurrealDB at http://localhost:8000 (root:root)"

infra-nitter: ## Start nitter-mcp (opt-in — needs Twitter accounts in infra/nitter/.env)
	$(INFRA_COMPOSE) up -d nitter-mcp
	@echo "nitter-mcp at http://127.0.0.1:41730/mcp (READ-ONLY Twitter GraphQL)"

infra-status: ## Show infra container status
	$(INFRA_COMPOSE) ps

infra-logs: ## Tail infra logs
	$(INFRA_COMPOSE) logs -f --tail=50

infra-down: ## Stop infra (data volumes preserved)
	$(INFRA_COMPOSE) down

infra-destroy: ## Stop infra AND wipe data volumes (irreversible)
	$(INFRA_COMPOSE) down -v
	@echo "Data volumes wiped. Next 'make infra' starts fresh."

# ── Sandbox (upstream OpenSandbox platform) ───────────────────────────────────
# No handrolled container. The OpenSandbox server (uv tool opensandbox-server)
# runs the Docker runtime on localhost:8080; dcode reaches it via the
# opensandbox MCP server (uv tool opensandbox-mcp, pinned mcp<2 — upstream
# 0.1.1 imports mcp.server.fastmcp, which mcp 2.x moved out). Insecure mode
# (no API key) is intentional for the local single-user box.
#   osb sandbox create --image python:3.12   # the upstream CLI, if you want it

SANDBOX_CONFIG ?= $(HOME)/.sandbox.toml
SANDBOX_PIDFILE ?= $(HOME)/.opensandbox/server.pid
SANDBOX_LOGFILE ?= $(HOME)/.opensandbox/server.log

sandbox-config: ## Generate ~/.sandbox.toml from the docker-runtime example (once)
	opensandbox-server init-config $(SANDBOX_CONFIG) --example docker

sandbox: ## Start the OpenSandbox server (docker runtime, http://localhost:8080)
	@[ -f $(SANDBOX_CONFIG) ] || opensandbox-server init-config $(SANDBOX_CONFIG) --example docker >/dev/null
	@if python3 -c "import urllib.request; urllib.request.urlopen('http://localhost:8080/openapi.json', timeout=2)" 2>/dev/null; then \
		echo "OpenSandbox server already running (localhost:8080)"; \
	else \
		mkdir -p $(HOME)/.opensandbox; \
		OPENSANDBOX_INSECURE_SERVER=YES nohup opensandbox-server --config $(SANDBOX_CONFIG) \
			>> $(SANDBOX_LOGFILE) 2>&1 & echo $$! > $(SANDBOX_PIDFILE); \
		sleep 1; echo "OpenSandbox server started (pid $$(cat $(SANDBOX_PIDFILE))), log: $(SANDBOX_LOGFILE)"; \
	fi

sandbox-status: ## Health-check the OpenSandbox server (:8080)
	@python3 -c "import urllib.request; urllib.request.urlopen('http://localhost:8080/openapi.json', timeout=3); print('OpenSandbox server UP (localhost:8080)')" \
		|| (echo "OpenSandbox server DOWN — run: make sandbox"; exit 1)

sandbox-stop: ## Stop the OpenSandbox server
	@if [ -f $(SANDBOX_PIDFILE) ] && kill -0 $$(cat $(SANDBOX_PIDFILE)) 2>/dev/null; then \
		kill $$(cat $(SANDBOX_PIDFILE)) && rm -f $(SANDBOX_PIDFILE) && echo "OpenSandbox server stopped"; \
	else \
		echo "No OpenSandbox server running (nothing to stop)"; rm -f $(SANDBOX_PIDFILE); \
	fi

# ── Profiles (scoped dcode sessions — 100% native API) ──────────────────────
# A profile is a dcode project root under profiles/<name>: scoped subagent
# roster (symlinked to the authored union in .deepagents/agents/), persona
# AGENTS.md, skills, and its own .mcp.json (only that lane's servers load).
# profiles/run.py drives dcode's own seams — ProjectContext,
# resolve_and_load_mcp_tools, create_cli_agent, run_textual_app — no patches.
# First launch per profile asks once whether to trust its MCP servers
# (persisted in ~/.deepagents/config.toml, scoped to the profile root).

DCODE_PY := $(HOME)/.local/share/uv/tools/deepagents-code/bin/python

coding: ## dcode · coding profile (7 agents; github/opensandbox/browser MCP)
	$(DCODE_PY) profiles/run.py coding

research: ## dcode · research profile (6 agents; research/surreal/browser MCP)
	$(DCODE_PY) profiles/run.py research

invest: ## dcode · invest profile (3 agents; equibles/research/surreal MCP)
	$(DCODE_PY) profiles/run.py invest

game: ## dcode · game profile (11 agents; godot/ray/surreal MCP)
	$(DCODE_PY) profiles/run.py game

media: ## dcode · media profile (5 agents; ray/surreal MCP)
	$(DCODE_PY) profiles/run.py media

social: ## dcode · social profile (4 agents; nitter/browser/surreal MCP)
	$(DCODE_PY) profiles/run.py social

profiles-check: ## Dry-run every profile (roster + skills + MCP scoping)
	@for p in coding research invest game media social; do \
		echo "── $$p ──"; \
		$(DCODE_PY) profiles/run.py $$p --dry-run 2>/dev/null | \
			grep -E "^(roster|skills|mcp servers|mcp tools|model)" ; \
	done

# Extra flags reach the launcher directly, e.g.:
#   $(DCODE_PY) profiles/run.py coding -M provider:model -m "fix the bug"

# ── Misc ────────────────────────────────────────────────────────────────────

hooks: ## Install pre-commit hooks (gitleaks secret scan)
	bash .github/hooks/install.sh

clean: ## Remove Python caches + .pyc files
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -name "*.pyc" -delete 2>/dev/null || true
