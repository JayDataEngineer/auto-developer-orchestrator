# OpenShell Namespace Architecture

## Overview

This document describes the **per-project namespace isolation** architecture used in the Auto-Developer Orchestrator for sandboxing AI agents.

---

## Architecture

### Per-Project Isolation Model

Each project runs in its own **OpenShell namespace** (sandbox), providing kernel-level isolation:

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
│  │  │             │  │             │  │             │   │  │
│  │  │  :8080      │  │  :8080      │  │  :8080      │   │  │
│  │  │  ✅         │  │  ✅         │  │  ✅         │   │  │
│  │  │             │  │             │  │             │   │  │
│  │  │  :3000      │  │  :3000      │  │  :3000      │   │  │
│  │  │  ✅         │  │  ✅         │  ✅         │   │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘   │  │
│  │       NS:              NS:            NS:            │  │
│  │   sample-project    test-repo        sandbox         │  │
│  │                                                       │  │
│  │  Filesystem: Landlock    Network: Namespace          │  │
│  │  Process: seccomp        Ports: Isolated per NS      │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Key Properties

| Property | Description |
|----------|-------------|
| **Namespace Name** | Derived from project directory basename (e.g., `sample-project`, `sandbox`) |
| **Filesystem** | Landlock-enforced isolation - each namespace can only access allowed paths |
| **Network** | Full network namespace isolation - each has its own interfaces, routing, socket space |
| **Process** | seccomp-protected process tree - privilege escalation blocked |
| **Port Binding** | **Same ports can be used across namespaces** (e.g., all can bind `:8080`) |
| **Multiple Agents** | Multiple agents within the same project share the namespace |

---

## Performance Characteristics

### Resource Overhead per Namespace

| Metric | Value | Notes |
|--------|-------|-------|
| **Startup Time** | ~100ms | Namespace creation + K3s pod scheduling |
| **Memory** | ~50-100MB | K3s pod baseline overhead |
| **Storage** | ~500MB-1GB | Container image + workspace files |
| **CPU** | Minimal when idle | Only consumes CPU during agent execution |
| **Network** | Isolated | Each namespace has independent network stack |

### Scaling Considerations

- **Max agents per project**: 5 (configurable in `pool.go`)
- **Max projects**: Limited by host memory (~8-10 projects with 100MB each = ~1GB overhead)
- **Namespace cleanup**: Idle namespaces persist (OpenShell manages lifecycle)

---

## Implementation Details

### Go Backend (`go-backend/internal/pi/client.go`)

```go
// Derive namespace from project directory name
namespace := filepath.Base(projectDir)

// Launch Pi inside OpenShell namespace
c.cmd = exec.CommandContext(
    c.ctx, openshellPath,
    "sandbox", "exec", c.namespace, "--",
    piPath, "--mode", "rpc",
)

// Rewrite LiteLLM URL for sandbox access
if c.sandboxed {
    for i, e := range env {
        if len(e) > len("LITELLM_PROXY_URL=") && e[:len("LITELLM_PROXY_URL=")] == "LITELLM_PROXY_URL=" {
            val := e[len("LITELLM_PROXY_URL="):]
            env[i] = "LITELLM_PROXY_URL=" + rewriteLocalhost(val) // host.docker.internal
        }
    }
}
```

**Key Changes:**
1. Namespace derived from project basename
2. `openshell sandbox exec <namespace>` command used
3. `LITELLM_PROXY_URL` rewritten to `host.docker.internal` for sandbox access
4. Namespace logged for debugging

### Frontend Display (`src/components/PiSessionCard.tsx`)

```tsx
{namespace && (
  <div className="flex items-center gap-1 px-1.5 py-0.5 border border-white/5 rounded bg-white/[0.02]">
    <Box size={8} className="text-muted-foreground" />
    <span className="text-[7px] font-mono text-muted-foreground uppercase tracking-wider">
      {namespace}
    </span>
  </div>
)}
```

**Visual Indicator:** Small badge showing the namespace name next to the model.

---

## Use Cases

### 1. Standard Coding Projects

Projects like `sample-project`, `test-repo` run in their own namespaces:
- Isolated file access
- Can run local dev servers on same ports
- Separate git operations

### 2. Computer Use Agent (`sandbox` project)

The dedicated `sandbox` project is designed for **computer interaction tasks**:

```bash
# Example: Ask sandbox to play a YouTube video
curl -X POST http://localhost:3847/api/pi/prompt \
  -H "Content-Type: application/json" \
  -d '{
    "project": "sandbox",
    "message": "Open Firefox and go to youtube.com, play the top trending video"
  }'
```

**Why a separate sandbox?**
- **Safety**: Untrusted GUI automation isolated from code projects
- **Persistence**: Downloads, browser data persist in sandbox namespace
- **Resources**: Can allocate more memory/CPU for browser automation
- **Policy**: Can apply different OpenShell policies (e.g., allow GUI apps)

**LiteLLM Integration:**
The sandbox automatically inherits LiteLLM configuration:
- `LITELLM_PROXY_URL=http://host.docker.internal:80`
- `LITELLM_MASTER_KEY=...`
- Access to all models configured in LiteLLM
- LangFuse tracing for observability

See [projects/sandbox/CONFIG.md](../projects/sandbox/CONFIG.md) for full LiteLLM setup.

### 3. Parallel Development

Run multiple agents on the same codebase without conflicts:
- Agent 1: Refactoring module A (namespace: `my-project`)
- Agent 2: Writing tests for module B (namespace: `my-project`)
- Both share the same namespace, same file access, same port bindings

---

## Security Model

### What's Protected

✅ **Host filesystem**: Read-only outside `/sandbox` and allowed paths  
✅ **Host network**: Outbound traffic can be policy-restricted  
✅ **Host processes**: Privilege escalation blocked via seccomp  
✅ **Cross-namespace access**: Namespaces cannot access each other  

### What's Not Protected

⚠️ **Within namespace**: Agent has full control inside its sandbox  
⚠️ **Persistent data**: Files saved persist across sessions  
⚠️ **Network egress**: Allowed by default (use policy to restrict)  
⚠️ **Resource exhaustion**: No per-namespace CPU/memory limits  

### Applying Custom Policies

```bash
# Create a restrictive policy for the sandbox project
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

---

## Troubleshooting

### Check Active Namespaces

```bash
# List all sandboxes
openshell sandbox list

# View sandbox details
openshell sandbox get sandbox
```

### View Logs

```bash
# Stream logs for a specific namespace
openshell logs sandbox --tail 100
```

### Connect for Debugging

```bash
# SSH into a sandbox namespace
openshell sandbox connect sandbox
```

### Restart a Namespace

```bash
# Delete and recreate
openshell sandbox delete sandbox
# Then send API request to recreate
curl -X POST http://localhost:3847/api/pi/agent/spawn \
  -d '{"project": "sandbox"}'
```

---

## Related Documentation

- [Sandbox Project README](../projects/sandbox/README.md) - Computer use agent guide
- [Pi Agent RPC Protocol](../go-backend/internal/pi/README.md) - Agent communication
- [Security Best Practices](./SECURITY.md) - Hardening guide

---

## FAQ

### Q: Can two projects bind to the same port?

**Yes!** Each namespace has its own socket space. Project A can bind to `:8080` and Project B can also bind to `:8080` without conflict.

### Q: Do agents in the same project share memory?

**Yes.** Multiple agents within the same project (e.g., `default`, `agent-123`) share the same namespace, filesystem, and network stack.

### Q: How do I isolate agents completely?

Put them in **different projects**. Each project gets its own namespace.

### Q: What happens to the namespace when the agent exits?

The namespace **persists**. OpenShell manages namespace lifecycle separately from agent processes. This allows quick agent restarts without namespace recreation overhead.

### Q: Can I limit CPU/memory per namespace?

Not currently. OpenShell provides isolation but not resource limits. For resource constraints, use Kubernetes resource quotas or Docker limits.
