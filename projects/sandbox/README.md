# Sandbox - Computer Use Agent

## Purpose

This is a **dedicated computer use agent** that runs in its own isolated OpenShell namespace. It's designed for tasks that require direct computer interaction:

- 🌐 **Web browsing**: "Open YouTube and play me a video"
- 📱 **App installation**: "Download and install Telegram Desktop"
- 🖥️ **GUI automation**: "Click the settings button"
- 🤖 **Bot operations**: "Run as a Telegram bot"

## Architecture

### OpenShell Namespace Isolation

The sandbox project runs in its own OpenShell namespace called `sandbox`:

```
┌─────────────────────────────────────────────────────────────┐
│                        Host Machine                         │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Docker Container (k3s cluster)                       │  │
│  │                                                       │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │  │
│  │  │  sample-    │  │  test-repo  │  │  sandbox    │   │  │
│  │  │  project    │  │             │  │  (computer) │   │  │
│  │  │  :8080      │  │  :8080      │  │  :8080      │   │  │
│  │  │  ✅         │  │  ✅         │  │  ✅         │   │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘   │  │
│  │       NS:              NS:            NS:            │  │
│  │   sample-project    test-repo        sandbox         │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

**Key Benefits:**
- **Port Isolation**: Sandbox can bind to `localhost:8080` without conflicting with other projects
- **Filesystem Isolation**: Landlock-enforced access control
- **Network Isolation**: Separate network namespace with custom policies
- **Process Isolation**: seccomp-protected process tree

### Performance Characteristics

| Metric | Value | Notes |
|--------|-------|-------|
| Startup Time | ~100ms | Namespace creation |
| Memory Overhead | ~50-100MB | K3s pod baseline |
| Storage | ~500MB-1GB | Container + workspace |
| CPU | Minimal when idle | Only during execution |

## Usage

### Via Frontend

1. Open the Auto-Developer Orchestrator UI
2. Select **"sandbox"** from the project dropdown
3. Send commands like:
   - "Open Firefox and go to youtube.com"
   - "Download Telegram Desktop from telegram.org"
   - "Play the top video on YouTube"

### Via API

```bash
# Spawn an agent in the sandbox namespace
curl -X POST http://localhost:3847/api/pi/agent/spawn \
  -H "Content-Type: application/json" \
  -d '{"project": "sandbox", "agentId": "computer-use"}'

# Send a prompt
curl -X POST http://localhost:3847/api/pi/prompt \
  -H "Content-Type: application/json" \
  -d '{
    "project": "sandbox",
    "agentId": "computer-use",
    "message": "Open YouTube and play the top trending video",
    "autoBranch": false
  }'
```

## Configuration

### LiteLLM Integration ✅

The sandbox is **pre-configured** to use your LiteLLM proxy:

- **URL**: `http://host.docker.internal:80` (auto-rewritten from `shared-traefik-1:80`)
- **Models**: All models configured in LiteLLM are available
- **Tracing**: LangFuse integration for full observability

See [CONFIG.md](./CONFIG.md) for detailed LiteLLM setup, model selection, and troubleshooting.

### OpenShell Policy (Optional)

For enhanced security, you can apply a custom policy:

```bash
# Create a policy file
cat > sandbox-policy.yaml <<EOF
filesystem:
  allow:
    - /sandbox/downloads/**
    - /tmp/**
    - ~/.config/**
network:
  allow:
    - "*.youtube.com/**"
    - "*.google.com/**"
    - "*.telegram.org/**"
process:
  allow:
    - firefox
    - chromium
    - wget
    - curl
EOF

# Apply the policy
openshell policy set sandbox --policy sandbox-policy.yaml
```

### Environment Variables

The sandbox namespace inherits these environment variables (with localhost rewritten):

- `LITELLM_PROXY_URL` → `http://host.docker.internal:80` (for model access)
- `LANGFUSE_HOST` → `http://host.docker.internal:80` (for tracing)
- `BROWSERLESS_URL` → `ws://orchestrator-browser:3000` (for browser automation)

## Security Considerations

### What's Protected

✅ **Host filesystem**: Read-only access outside `/sandbox`  
✅ **Network**: Outbound traffic can be policy-restricted  
✅ **Processes**: Privilege escalation blocked via seccomp  

### What's Not Protected

⚠️ **Within the namespace**: The agent has full control inside its sandbox  
⚠️ **Persistent data**: Files saved in `/sandbox` persist across sessions  
⚠️ **Network egress**: Allowed by default (use policy to restrict)  

### Best Practices

1. **Use for untrusted tasks**: Run experimental AI commands here
2. **Apply network policies**: Restrict which domains the agent can access
3. **Monitor via logs**: `openshell logs sandbox --tail`
4. **Clean up regularly**: Remove downloaded files after use

## Troubleshooting

### Check if sandbox namespace is running

```bash
openshell sandbox list
```

### View sandbox logs

```bash
openshell logs sandbox --tail 100
```

### Connect to sandbox (debug)

```bash
openshell sandbox connect sandbox
```

### Restart sandbox namespace

```bash
openshell sandbox delete sandbox
# Then send a new API request to recreate it
```

## Related Documentation

- [OpenShell Namespace Architecture](../../docs/SANDBOX_NAMESPACES.md)
- [Pi Agent RPC Protocol](../../go-backend/internal/pi/README.md)
- [Security Best Practices](../../docs/SECURITY.md)
