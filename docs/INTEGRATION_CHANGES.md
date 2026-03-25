# Shared Infrastructure Integration - Changes Summary

## Overview

This project has been integrated with **shared-docker-infra** to use centralized services for:
- **Traefik** - Reverse proxy and routing
- **LiteLLM** - Unified AI gateway (LLM, embeddings, ASR, TTS)
- **Langfuse** - Observability and tracing

## Files Modified

### 1. `docker-compose.go.yml` (Production)

**Before:**
- Used Nginx as reverse proxy (ports 80/443)
- Isolated `orchestrator-network` (bridge)
- LiteLLM pointed to internal container `http://litellm:4000`
- Optional bundled LiteLLM service (commented out)

**After:**
- ✅ Removed Nginx service
- ✅ Uses external `shared-infra` network
- ✅ Traefik labels for automatic routing
- ✅ LiteLLM via Traefik: `http://litellm.local`
- ✅ Langfuse environment variables added
- ✅ No exposed ports (all via Traefik)

```yaml
networks:
  shared-infra:
    external: true
    name: ${SHARED_NETWORK_NAME:-shared-infra}

services:
  go-backend:
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.orchestrator.rule=Host(`orchestrator.local`)"
    environment:
      - LITELLM_PROXY_URL=http://litellm.local
      - LANGFUSE_HOST=http://langfuse.local
```

### 2. `docker-compose.dev.go.yml` (Development)

**Before:**
- Isolated `orchestrator-network` (bridge)
- Direct port exposure (3847, 8080, 5173)
- No Traefik integration

**After:**
- ✅ Uses external `shared-infra` network
- ✅ Traefik labels for all services
- ✅ Access via domains (orchestrator.local, orchestrator-go.local)
- ✅ Langfuse integration for all services

```yaml
services:
  go-backend:
    labels:
      - "traefik.http.routers.orchestrator-go-dev.rule=Host(`orchestrator-go.local`)"
  
  frontend:
    labels:
      - "traefik.http.routers.orchestrator-frontend.rule=Host(`orchestrator.local`)"
```

### 3. `docker-compose.dev.yml` (Node.js Development)

**Before:**
- Isolated `orchestrator-network`
- Direct port exposure

**After:**
- ✅ Uses external `shared-infra` network
- ✅ Traefik labels for routing
- ✅ Shared infrastructure integration

### 4. `.env.example`

**Added:**
```bash
# Shared Infrastructure Configuration
SHARED_NETWORK_NAME="shared-infra"
INFRA_DOMAIN="local"

# LiteLLM (via Traefik)
LITELLM_PROXY_URL="http://litellm.local"

# Langfuse Observability
LANGFUSE_HOST="http://langfuse.local"
LANGFUSE_PUBLIC_KEY=""
LANGFUSE_SECRET_KEY=""
```

### 5. `README.md`

**Updated:**
- ✅ Added prerequisites section (shared-docker-infra requirement)
- ✅ Updated architecture diagram (Traefik instead of Nginx)
- ✅ Updated quick start with shared infra setup
- ✅ Updated environment variables table
- ✅ Updated Docker deployment section

### 6. New Files

#### `setup.sh`
Automated setup script that:
- Checks for shared-docker-infra installation
- Verifies `shared-infra` network exists
- Starts core services if needed
- Creates `.env` from `.env.example`

#### `docs/SHARED_INFRA_INTEGRATION.md`
Complete integration guide covering:
- Architecture overview
- Prerequisites and setup
- Configuration details
- LiteLLM integration
- Langfuse observability
- Troubleshooting

#### `docs/SHARED_INFRA_QUICKREF.md`
Quick reference card with:
- Common commands
- Access URLs
- Environment variables
- Troubleshooting tips

## Architecture Changes

### Before

```
┌─────────────────────────────────────────────────────────────┐
│                    Nginx (Port 80/443)                      │
│  - Self-managed reverse proxy                               │
└────────────────────┬────────────────────────────────────────┘
                     │
     ┌───────────────┴───────────────┐
     │                               │
     ▼                               ▼
┌─────────────┐              ┌─────────────┐
│ Go Backend  │              │ Python      │
│ Port 3847   │              │ Agent       │
│ (exposed)   │              │ Port 8080   │
│             │              │ (exposed)   │
└─────────────┘              └─────────────┘

Isolated network: orchestrator-network
```

### After

```
┌─────────────────────────────────────────────────────────────┐
│         Traefik (shared-docker-infra)                        │
│  - Centralized reverse proxy                                 │
│  - http://orchestrator.local → orchestrator-go:3847         │
└────────────────────┬────────────────────────────────────────┘
                     │
     ┌───────────────┴───────────────┐
     │                               │
     ▼                               ▼
┌─────────────┐              ┌─────────────┐
│ Go Backend  │              │ Python      │
│ Port 3847   │              │ Agent       │
│ (internal)  │              │ Port 8080   │
│             │              │ (internal)  │
└──────┬──────┘              └─────────────┘
       │
       │ (via Traefik)
       ▼
┌─────────────────────────────────────────────────────────────┐
│           Shared Infrastructure Services                     │
│  - LiteLLM (http://litellm.local) - AI gateway             │
│  - Langfuse (http://langfuse.local) - Observability        │
└─────────────────────────────────────────────────────────────┘

Shared network: shared-infra (external)
```

## Benefits

1. **Centralized routing** - Single Traefik instance for all services
2. **Unified AI gateway** - Shared LiteLLM for all AI needs (LLM, ASR, TTS, embeddings)
3. **Observability** - Automatic Langfuse tracing for all AI calls
4. **Resource efficiency** - No duplicate services (Nginx, LiteLLM)
5. **Consistent domains** - All services use `.local` domains
6. **Simplified networking** - Single external network

## Migration Steps

### For Existing Deployments

1. **Stop current services:**
   ```bash
   make prod-down
   ```

2. **Start shared-docker-infra:**
   ```bash
   cd ~/Documents/programs/shared-docker-infra
   task core:up
   ```

3. **Update environment:**
   ```bash
   cp .env.example .env
   # Edit .env with your API keys and Langfuse credentials
   ```

4. **Start with new configuration:**
   ```bash
   make prod-up
   ```

5. **Verify:**
   ```bash
   curl http://orchestrator.local/api/health
   open http://orchestrator.local
   ```

## Testing

### Health Checks

```bash
# Orchestrator
curl http://orchestrator.local/api/health

# LiteLLM
curl http://litellm.local/health

# Langfuse
curl http://langfuse.local/api/health
```

### Model Access

```bash
# List available models
curl http://litellm.local/v1/models \
  -H "Authorization: Bearer sk-litellm-master"

# Test chat completion
curl http://litellm.local/v1/chat/completions \
  -H "Authorization: Bearer sk-litellm-master" \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen-35-27", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Langfuse Tracing

1. Open http://langfuse.local
2. Go to Settings > API Keys
3. Copy keys to `.env`:
   ```bash
   LANGFUSE_PUBLIC_KEY="pk-lf-xxx"
   LANGFUSE_SECRET_KEY="sk-lf-xxx"
   ```
4. Restart services
5. View traces at http://langfuse.local/traces

## Rollback Plan

If you need to revert to standalone mode:

1. **Restore old docker-compose.go.yml:**
   ```bash
   git checkout HEAD~1 docker-compose.go.yml
   ```

2. **Uncomment bundled LiteLLM:**
   ```yaml
   services:
     litellm:
       image: ghcr.io/berriai/litellm:main-latest
       ports:
         - "4000:4000"
   ```

3. **Update .env:**
   ```bash
   LITELLM_PROXY_URL="http://localhost:4000"
   ```

4. **Restart:**
   ```bash
   make prod-down && make prod-up
   ```

## Next Steps

- [ ] Configure Langfuse credentials in `.env`
- [ ] Test LiteLLM model access
- [ ] Verify Langfuse tracing is working
- [ ] Update CI/CD pipelines for shared infra
- [ ] Document Langfuse dashboard usage for team

## Support

- **Integration Guide:** `docs/SHARED_INFRA_INTEGRATION.md`
- **Quick Reference:** `docs/SHARED_INFRA_QUICKREF.md`
- **shared-docker-infra:** `~/Documents/programs/shared-docker-infra/README.md`
