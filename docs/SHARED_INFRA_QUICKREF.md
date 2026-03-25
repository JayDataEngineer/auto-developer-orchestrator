# Shared Infrastructure - Quick Reference

## Quick Start

```bash
# 1. Start shared-docker-infra first
cd ~/Documents/programs/shared-docker-infra
task core:up

# 2. Setup this project
cd ~/Documents/programs/dev/auto-developer-orchestrator
./setup.sh

# 3. Configure .env (add API keys and Langfuse credentials)

# 4. Start services
make dev-up    # Development
make prod-up   # Production
```

## Access URLs

| Service | URL | Purpose |
|---------|-----|---------|
| Orchestrator | http://orchestrator.local | Main dashboard |
| Langfuse | http://langfuse.local | Observability & traces |
| LiteLLM | http://litellm.local | AI gateway API |
| Traefik | http://traefik.local | Reverse proxy dashboard |

## Key Commands

```bash
# Health checks
curl http://orchestrator.local/api/health
curl http://litellm.local/health
curl http://langfuse.local/api/health

# List models (via LiteLLM)
curl http://litellm.local/v1/models \
  -H "Authorization: Bearer sk-litellm-master"

# Test chat completion
curl http://litellm.local/v1/chat/completions \
  -H "Authorization: Bearer sk-litellm-master" \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen-35-27", "messages": [{"role": "user", "content": "Hello"}]}'

# View logs
make logs
docker compose -f docker-compose.go.yml logs -f
```

## Environment Variables

```bash
# Required
JULES_API_KEY=""
GITHUB_TOKEN=""
OPENAI_API_KEY=""

# Shared Infrastructure
SHARED_NETWORK_NAME="shared-infra"
INFRA_DOMAIN="local"

# LiteLLM (via Traefik)
LITELLM_PROXY_URL="http://litellm.local"

# Langfuse (get from http://langfuse.local)
LANGFUSE_HOST="http://langfuse.local"
LANGFUSE_PUBLIC_KEY="pk-lf-xxx"
LANGFUSE_SECRET_KEY="sk-lf-xxx"
```

## Troubleshooting

```bash
# Check shared-infra network
docker network inspect shared-infra

# Check Traefik routing
curl http://traefik.local/api/http/routers

# Restart shared services
cd ~/Documents/programs/shared-docker-infra
task core:down && task core:up

# Restart this project
make dev-down && make dev-up
```

## Architecture

```
Internet → Traefik → orchestrator-go:3847 → python-agent:8080
                    ↓
              litellm.local (AI gateway)
                    ↓
              langfuse.local (traces)
```

## File Changes Summary

| File | Change |
|------|--------|
| `docker-compose.go.yml` | Removed Nginx, added Traefik labels, use shared-infra network |
| `docker-compose.dev.go.yml` | Use shared-infra network, Traefik routing |
| `.env.example` | Added Langfuse and shared infra config |
| `README.md` | Updated for shared infra integration |
| `setup.sh` | New script to setup integration |
| `docs/SHARED_INFRA_INTEGRATION.md` | New integration guide |
