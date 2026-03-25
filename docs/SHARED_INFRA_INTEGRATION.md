# Shared Infrastructure Integration Guide

This document explains how auto-developer-orchestrator integrates with shared-docker-infra for unified AI services and observability.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│         Traefik (shared-docker-infra)                        │
│  Reverse Proxy & Router                                      │
│  - http://orchestrator.local → orchestrator-go:3847         │
│  - http://litellm.local → litellm:4000                       │
│  - http://langfuse.local → langfuse-web:3000                 │
└────────────────────┬────────────────────────────────────────┘
                     │
     ┌───────────────┴───────────────┐
     │                               │
     ▼                               ▼
┌─────────────┐              ┌─────────────┐
│ Go Backend  │              │ Python      │
│ Port 3847   │              │ Agent       │
│             │              │ Port 8080   │
│ - REST API  │              │ - LangChain │
│ - Git Ops   │              │ - Deep      │
│ - Jules API │              │   Agents    │
│ - SQLite    │              │             │
└──────┬──────┘              └─────────────┘
       │
       │ (via Traefik routing)
       ▼
┌─────────────────────────────────────────────────────────────┐
│           Shared Infrastructure Services                     │
│  - LiteLLM (http://litellm.local) - Unified AI gateway      │
│  - Langfuse (http://langfuse.local) - Observability & traces│
│  - llama.cpp (internal) - Local LLM inference               │
│  - Whisper (internal) - ASR service                         │
│  - Kokoro TTS (internal) - Text-to-speech                   │
└─────────────────────────────────────────────────────────────┘
```

## Prerequisites

1. **shared-docker-infra** must be installed and running:
   ```bash
   cd ~/Documents/programs/shared-docker-infra
   task core:up
   ```

2. **Required services** in shared-docker-infra:
   - Traefik (reverse proxy)
   - LiteLLM (AI gateway)
   - Langfuse (observability) - optional but recommended

## Quick Start

### 1. Run Setup Script

```bash
./setup.sh
```

This script:
- Checks for shared-docker-infra installation
- Verifies the `shared-infra` Docker network exists
- Starts core services if needed
- Creates `.env` from `.env.example`

### 2. Configure Environment

Edit `.env` and set:

```bash
# API Keys
JULES_API_KEY="your-jules-key"
GITHUB_TOKEN="your-github-token"
OPENAI_API_KEY="your-openai-key"

# Langfuse (get from http://langfuse.local > Settings > API Keys)
LANGFUSE_PUBLIC_KEY="pk-lf-xxxxxxxx"
LANGFUSE_SECRET_KEY="sk-lf-xxxxxxxx"
LANGFUSE_HOST="http://langfuse.local"

# LiteLLM (via Traefik)
LITELLM_PROXY_URL="http://litellm.local"

# Network configuration
SHARED_NETWORK_NAME="shared-infra"
INFRA_DOMAIN="local"
```

### 3. Start Services

```bash
# Development (with hot reload)
make dev-up

# Production
make prod-up
```

### 4. Access the Application

- **Dashboard**: http://orchestrator.local
- **API**: http://orchestrator.local/api
- **Langfuse**: http://langfuse.local
- **LiteLLM**: http://litellm.local

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SHARED_NETWORK_NAME` | `shared-infra` | Docker network name |
| `INFRA_DOMAIN` | `local` | Domain suffix for Traefik |
| `LITELLM_PROXY_URL` | `http://litellm.local` | LiteLLM endpoint |
| `LANGFUSE_HOST` | `http://langfuse.local` | Langfuse endpoint |
| `LANGFUSE_PUBLIC_KEY` | - | Langfuse public key |
| `LANGFUSE_SECRET_KEY` | - | Langfuse secret key |

### Docker Networks

The project uses the **external** `shared-infra` network:

```yaml
networks:
  shared-infra:
    external: true
    name: ${SHARED_NETWORK_NAME:-shared-infra}
```

This allows containers to communicate with:
- Traefik (reverse proxy)
- LiteLLM (AI gateway)
- Langfuse (observability)
- Other shared services

### Traefik Labels

Services are automatically discovered by Traefik via labels:

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.orchestrator.rule=Host(`orchestrator.local`)"
  - "traefik.http.routers.orchestrator.entrypoints=http"
  - "traefik.http.services.orchestrator.loadbalancer.server.port=3847"
```

## LiteLLM Integration

### Configuration

LiteLLM provides a unified OpenAI-compatible API for all AI models:

```bash
# Test connection
curl http://litellm.local/v1/models \
  -H "Authorization: Bearer sk-litellm-master"

# Chat completion
curl http://litellm.local/v1/chat/completions \
  -H "Authorization: Bearer sk-litellm-master" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen-35-27",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Available Models

See shared-docker-infra documentation for the full list:

| Model | Type | Backend |
|-------|------|---------|
| `qwen-35-27` | Chat | Local (llama.cpp) |
| `qwen-35-27-fast` | Chat | Local (llama.cpp) |
| `zai-glm-4-5` | Chat | Cloud (ZAI) |
| `jina-v5-embed` | Embeddings | Local |
| `distil-whisper` | ASR | Local (whisper) |
| `kokoro` | TTS | Local (kokoro) |

### Usage in Go Backend

```go
// Example: Call LiteLLM from Go
resp, err := http.Post(
    os.Getenv("LITELLM_PROXY_URL")+"/v1/chat/completions",
    "application/json",
    bytes.NewBuffer(requestBody),
)
```

## Langfuse Observability

### Automatic Tracing

All LiteLLM calls are automatically traced to Langfuse when configured:

```yaml
# In docker-compose.yml
environment:
  - LANGFUSE_HOST=http://langfuse.local
  - LANGFUSE_PUBLIC_KEY=pk-lf-xxx
  - LANGFUSE_SECRET_KEY=sk-lf-xxx
```

### View Traces

1. Open http://langfuse.local
2. Navigate to **Traces**
3. Filter by session, user, or tags

### Manual Tracing (Python)

```python
from langfuse import Langfuse

langfuse = Langfuse(
    public_key=os.getenv("LANGFUSE_PUBLIC_KEY"),
    secret_key=os.getenv("LANGFUSE_SECRET_KEY"),
    host=os.getenv("LANGFUSE_HOST")
)

# Create trace
trace = langfuse.trace(
    name="chat-completion",
    user_id="user-123",
    session_id="session-456"
)

# Add generation
generation = trace.generation(
    name="response",
    model="qwen-35-27",
    input=[{"role": "user", "content": "Hello!"}],
    output="Hi there!"
)
```

## Troubleshooting

### Can't Access http://orchestrator.local

1. **Check Traefik is running**:
   ```bash
   docker ps | grep traefik
   ```

2. **Verify DNS resolution**:
   ```bash
   # Add to /etc/hosts if needed
   echo "127.0.0.1 orchestrator.local" | sudo tee -a /etc/hosts
   ```

3. **Check service health**:
   ```bash
   docker ps | grep orchestrator
   ```

### LiteLLM Connection Failed

1. **Check LiteLLM is running**:
   ```bash
   docker ps | grep litellm
   ```

2. **Test direct connection**:
   ```bash
   curl http://litellm.local/health
   ```

3. **Verify network connectivity**:
   ```bash
   docker network inspect shared-infra
   ```

### Langfuse Traces Not Appearing

1. **Verify credentials**:
   ```bash
   # Get keys from http://langfuse.local > Settings > API Keys
   echo $LANGFUSE_PUBLIC_KEY
   echo $LANGFUSE_SECRET_KEY
   ```

2. **Check Langfuse health**:
   ```bash
   curl http://langfuse.local/api/health
   ```

3. **Test trace manually**:
   ```python
   from langfuse import Langfuse
   langfuse = Langfuse(...)
   trace = langfuse.trace(name="test")
   langfuse.flush()
   ```

### Network Issues

1. **Recreate network**:
   ```bash
   docker network rm shared-infra
   cd ~/Documents/programs/shared-docker-infra
   docker compose up -d
   ```

2. **Restart services**:
   ```bash
   make dev-down && make dev-up
   ```

## Migration from Standalone

If you were previously running standalone (without shared-docker-infra):

### Before

```yaml
# Old configuration
networks:
  orchestrator-network:
    driver: bridge

services:
  litellm:
    image: ghcr.io/berriai/litellm:main-latest
    ports:
      - "4000:4000"
```

### After

```yaml
# New configuration
networks:
  shared-infra:
    external: true
    name: ${SHARED_NETWORK_NAME:-shared-infra}

services:
  # LiteLLM removed - use shared instance
  go-backend:
    environment:
      - LITELLM_PROXY_URL=http://litellm.local
```

## Best Practices

1. **Always start shared-docker-infra first**:
   ```bash
   cd ~/Documents/programs/shared-docker-infra
   task core:up
   ```

2. **Use Traefik domains, not direct ports**:
   - ✅ `http://litellm.local`
   - ❌ `http://localhost:4000`

3. **Enable Langfuse tracing** for observability

4. **Use environment variables** for configuration

5. **Keep shared-infra network clean**:
   ```bash
   docker network prune
   ```

## Related Documentation

- [shared-docker-infra README](../../shared-docker-infra/README.md)
- [LiteLLM Integration](../../shared-docker-infra/litellm/README.md)
- [Langfuse Integration](../../shared-docker-infra/langfuse/README.md)
- [Traefik Configuration](../../shared-docker-infra/traefik/README.md)
