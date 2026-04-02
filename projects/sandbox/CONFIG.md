# Sandbox Configuration - Computer Use Agent

## LiteLLM Integration

The sandbox project is **pre-configured** to use your LiteLLM proxy for all AI model calls.

### Environment Variables (Auto-Injected)

When the sandbox agent runs inside its OpenShell namespace, it automatically receives:

```bash
LITELLM_PROXY_URL=http://host.docker.internal:80
LITELLM_MASTER_KEY=sk-litellm-e2e-test-master-key
```

**How it works:**
1. Go backend sets `LITELLM_PROXY_URL=http://shared-traefik-1:80` (Docker network)
2. `client.go` rewrites localhost URLs to `host.docker.internal` for sandbox access
3. Pi agent inside sandbox uses LiteLLM for all model calls

### Verify LiteLLM Access

```bash
# From inside the sandbox namespace
openshell sandbox connect sandbox

# Test LiteLLM connectivity
curl -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  "$LITELLM_PROXY_URL/v1/models"
```

Expected output: List of available models from your LiteLLM configuration.

### Available Models

The sandbox agent can access **all models configured in LiteLLM**:

```bash
# Send a prompt - it will use the default model (or specify one)
curl -X POST http://localhost:3847/api/pi/prompt \
  -H "Content-Type: application/json" \
  -d '{
    "project": "sandbox",
    "message": "Open YouTube and play a video",
    "model": "gpt-4"  # Any model in LiteLLM
  }'
```

### Model Selection via Frontend

1. Open the orchestrator UI
2. Select **"sandbox"** project
3. Click on an agent card
4. Use the model dropdown to select any LiteLLM-configured model

---

## Pi Agent Tools

The Pi agent has access to these tools (via LiteLLM function calling):

### Code/Development Tools
- `read_file` - Read file contents
- `write_file` - Write/create files
- `edit_file` - Edit existing files
- `search_code` - Search codebase
- `run_command` - Execute shell commands (sandboxed)

### Computer Use Tools (Sandbox-Specific)
- `browser_navigate` - Open URLs in browser
- `browser_click` - Click elements on page
- `browser_type` - Type into input fields
- `screenshot` - Capture screen region
- `launch_application` - Start GUI apps (if allowed by policy)

### Web Tools
- `web_search` - Search the web
- `fetch_url` - Fetch webpage content
- `download_file` - Download files to sandbox

---

## Customizing LiteLLM Access

### Option 1: Per-Project Model Configuration

Create `projects/sandbox/.pi/agent/models.json`:

```json
{
  "models": [
    {
      "provider": "litellm",
      "id": "gpt-4",
      "name": "GPT-4 (Premium)"
    },
    {
      "provider": "litellm",
      "id": "claude-3-5-sonnet",
      "name": "Claude 3.5 Sonnet"
    },
    {
      "provider": "litellm",
      "id": "gemini-2.0-flash",
      "name": "Gemini 2.0 Flash (Fast)"
    }
  ],
  "defaultModel": "gemini-2.0-flash"
}
```

### Option 2: Environment Variable Override

In `docker-compose.yml`, add sandbox-specific config:

```yaml
go-backend:
  environment:
    # ... existing vars ...
    - SANDBOX_LITELLM_URL=http://shared-traefik-1:80
    - SANDBOX_LITELLM_KEY=${LITELLM_MASTER_KEY}
```

### Option 3: OpenShell Policy with LiteLLM Allowlist

Create `sandbox-policy.yaml` to ensure LiteLLM access:

```yaml
filesystem:
  allow:
    - /sandbox/**
    - /tmp/**
network:
  allow:
    # Allow LiteLLM proxy access
    - "shared-traefik-1:80"
    - "host.docker.internal:*"
    # Allow web access for computer use
    - "*.youtube.com/**"
    - "*.google.com/**"
process:
  allow:
    - firefox
    - chromium
    - wget
    - curl
```

Apply the policy:

```bash
openshell policy set sandbox --policy sandbox-policy.yaml
```

---

## LangFuse Tracing

If LangFuse is configured, all sandbox agent interactions are traced:

```bash
# Environment variables (auto-inherited)
LANGFUSE_HOST=http://host.docker.internal:80
LANGFUSE_PUBLIC_KEY=pk-lf-...
LANGFUSE_SECRET_KEY=sk-lf-...
```

**View traces:**
1. Open LangFuse UI (usually `http://langfuse.local`)
2. Filter by `project=sandbox` or `agentId=*`
3. See full execution traces including:
   - Model calls (via LiteLLM)
   - Tool executions
   - Token usage
   - Latency metrics

---

## Browserless Integration

For browser automation, the sandbox can access the Browserless service:

```bash
# Environment variable (auto-inherited)
BROWSERLESS_URL=ws://orchestrator-browser:3000
```

**Example: Browser automation via Pi agent**

```bash
curl -X POST http://localhost:3847/api/pi/prompt \
  -H "Content-Type: application/json" \
  -d '{
    "project": "sandbox",
    "message": "Use browserless to navigate to youtube.com and play the first video"
  }'
```

---

## Troubleshooting

### LiteLLM Connection Refused

**Problem:** `Connection refused on port 80`

**Solution:**
1. Check LiteLLM is running: `docker ps | grep litellm`
2. Verify network: `docker network inspect shared-infra`
3. Test from host: `curl http://localhost:80/v1/models`
4. Check `host.docker.internal` resolution inside sandbox

### Models Not Showing in UI

**Problem:** Model dropdown is empty

**Solution:**
```bash
# Check available models via API
curl http://localhost:3847/api/pi/models?project=sandbox
```

### Agent Can't Access Browserless

**Problem:** Browser automation fails

**Solution:**
1. Verify Browserless is running: `docker ps | grep browserless`
2. Check WebSocket URL: `echo $BROWSERLESS_URL`
3. Test connection: `curl ws://orchestrator-browser:3000`

---

## Related Documentation

- [LiteLLM Configuration](../../shared-docker-infra/docs/services/litellm.md)
- [Pi Agent Tools](../../go-backend/internal/pi/README.md)
- [OpenShell Policies](./SANDBOX_POLICIES.md)
