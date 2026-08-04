# ─────────────────────────────────────────────────────────────────────────────
# Nitter MCP — read-only Twitter via direct GraphQL (twscrape).
#
# Single-service, stateless_http=True. Pairs with the orchestrator's MCP
# tool catalog (orgs/_shared/tool_servers.yaml → nitter entry).
#
# Build:
#   docker build -t orchestrator-nitter-mcp:latest infra/nitter/
# Run (standalone):
#   docker run -p 41730:41730 --env-file infra/nitter/.env \
#     -v nitter-db:/data orchestrator-nitter-mcp:latest
# Or via the orchestrator stack:
#   docker compose -f docker-compose.infra.yml up -d nitter-mcp
# ─────────────────────────────────────────────────────────────────────────────

ARG PYTHON_VERSION=3.12
FROM python:${PYTHON_VERSION}-slim-bookworm AS builder

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    UV_LINK_MODE=copy \
    UV_PROJECT_ENVIRONMENT=/app/.venv

# Build deps for any C extensions in twscrape's transitive tree (lxml, etc.)
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# uv for fast dependency install (mirrors infra/media-mcp pattern).
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

WORKDIR /app

# Install deps first (better layer caching — source changes don't bust deps).
COPY pyproject.toml ./
RUN uv sync --no-dev --no-install-project

COPY src/ ./src/

# ── Runtime stage ───────────────────────────────────────────────────────────
FROM python:${PYTHON_VERSION}-slim-bookworm AS runtime

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PATH="/app/.venv/bin:$PATH" \
    PYTHONPATH="/app"

# tini for proper PID-1 signal handling (so SIGTERM reaches Python and the
# twscrape pool closes cleanly — avoids locked SQLite DB on container stop).
RUN apt-get update && apt-get install -y --no-install-recommends tini \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /data

WORKDIR /app

COPY --from=builder /app/.venv /app/.venv
COPY --from=builder /app/src /app/src

# twscrape SQLite DB persists here (mounted as a named volume in compose).
VOLUME ["/data"]

EXPOSE 41730

# Cheap liveness — GET /health is a plain HTTP route (no MCP round-trip).
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["python", "-c", "import sys,urllib.request; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:41730/health', timeout=3).status == 200 else 1)"]

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["python", "-m", "src.server"]
