# Cache & Parallelism — The Sub-Agent Slot Problem

Date: 2026-05-10

---

## The Problem

`delegate_async` is lying. It claims to run sub-agents in parallel. On local llama-server, it doesn't.

### What We Thought Was Happening

```
delegate_async(task1, task2, task3)
  → goroutine 1: sub-agent runs task1
  → goroutine 2: sub-agent runs task2
  → goroutine 3: sub-agent runs task3
  → collect_results() merges all three
```

### What's Actually Happening

```
delegate_async(task1, task2, task3)
  → goroutine 1: sub-agent calls Adapter.StreamChat()
    → a.mu.Lock() ← blocks
    → sends messages to llama-server slot
    → a.mu.Unlock()
  → goroutine 2: sub-agent calls Adapter.StreamChat()
    → a.mu.Lock() ← was waiting
    → REPLACES messages in session
    → KV cache from goroutine 1 gets trashed
    → a.mu.Unlock()
  → goroutine 3: same pattern
  → All three run serially. KV cache rebuilt every call.
```

Three problems in one:

1. **Not parallel.** `Adapter.mu` serializes all calls through one session.
2. **KV cache thrashing.** Each sub-agent replaces the full message array. The CTO's warm KV cache gets evicted and rebuilt on every sub-agent call.
3. **Potential OOM.** If we fix #1 by giving each sub-agent its own Adapter/session, each session claims its own llama-server slot with its own KV cache. 3 sub-agents + CTO = 4 slots. On a 4090 with 13GB model, that's tight.

---

## The Math (RTX 4090, 24GB VRAM)

```
Model weights:              ~13 GB  (Gemma 4 26B-A4B, IQ4_NL)
Remaining VRAM:             ~11 GB
KV cache per slot (4K ctx): ~0.3 GB  (MoE, 4B active params)
KV cache per slot (16K ctx):~1.2 GB
KV cache per slot (32K ctx):~2.4 GB
```

Current config:
- CTO: 32K context → ~2.4 GB KV cache
- Sub-agents: 16K context → ~1.2 GB KV cache each

Budget:
```
CTO (32K):        2.4 GB
Sub-agent (16K):  1.2 GB × N
─────────────────────────
Available:        11 GB
```

| Sub-agents | KV Cache Total | Fits? |
|-----------|---------------|-------|
| 1         | 3.6 GB        | Yes   |
| 2         | 4.8 GB        | Yes   |
| 3         | 6.0 GB        | Tight |
| 4         | 7.2 GB        | Risky |
| 5+        | 8.4+ GB       | OOM likely |

**The realistic limit on a 4090 is 2 concurrent sub-agents alongside the CTO.**

With smaller sub-agent context (4K instead of 16K), we can fit more:
```
CTO (32K):        2.4 GB
Sub-agent (4K):   0.3 GB × N
```

| Sub-agents (4K) | Total | Fits? |
|----------------|-------|-------|
| 5              | 3.9 GB | Yes   |
| 10             | 5.4 GB | Yes   |
| 20             | 8.4 GB | Tight |

**But 4K context is too small for research sub-agents that get large scrape results.** That's why we bumped it to 16K.

---

## The Architecture Issue

There are two separate concerns getting tangled:

### 1. Session Isolation (correctness)
Each agent needs its own message history. Currently `subSession` handles this in Go memory — each sub-agent has its own `[]Message`. But they all funnel through the same `Adapter` which holds a single `llama.Session` with a single `session_id`.

### 2. Slot Management (resource)
llama-server slots are a scarce GPU resource. Currently we don't manage them — the Adapter creates one session per instance and holds it forever.

These need to be separate. Session isolation is a Go-level concern. Slot management is a GPU-level concern.

---

## Solution: Slot Pool with Concurrency Limit

### Core Concept

```
SlotPool
├── Slot 0: CTO (persistent, warm KV cache, 32K)
├── Slot 1: available for sub-agents (16K)
├── Slot 2: available for sub-agents (16K)
└── Slot 3: reserved (OOM headroom)
```

The CTO keeps its dedicated slot. Sub-agents borrow from a pool. When the pool is empty, new sub-agents either queue or fall back to cloud.

### Interface

```go
// SlotPool manages llama-server slots as a bounded resource.
type SlotPool interface {
    // Acquire gets a slot for a sub-agent. Blocks if pool is empty.
    // Returns a Session that must be Released when done.
    Acquire(ctx context.Context, ctxSize int) (*BorrowedSession, error)

    // Release returns a slot to the pool. Closes the KV cache.
    Release(session *BorrowedSession)

    // MaxConcurrent returns the number of available sub-agent slots.
    MaxConcurrent() int
}

// BorrowedSession wraps a llama.Session with pool-managed lifecycle.
type BorrowedSession struct {
    Session *llama.Session
    done    chan struct{}
}

func (s *BorrowedSession) Done() {
    close(s.done)
}
```

### Wiring

```go
// At startup:
pool := NewSlotPool(engine, maxSubSlots)

// CTO gets its own Adapter (persistent slot):
ctoAdapter := llm.NewAdapter(engine, 32768)

// Sub-agents get Adapters from the pool:
func createSubAgentProvider(pool SlotPool) core.LLMProvider {
    return &PooledProvider{pool: pool, ctxSize: 16384}
}

// PooledProvider.Acquire on first StreamChat, Release when sub-agent finishes.
```

### Fallback Chain

When the pool is empty:
1. **Wait** (if `delegate_to` — synchronous, user is waiting)
2. **Queue** (if `delegate_async` — can wait for a slot to free up)
3. **Fall back to cloud** (if timeout waiting for slot — use Gemini/OpenRouter)

This is the same engine fallback we already have, but triggered by resource contention instead of server failure.

---

## Impact on Current Code

### What Changes

| Component | Change | Scope |
|-----------|--------|-------|
| `Adapter` | Stays as-is for CTO (single persistent session) | None |
| `ParallelRunner` | Gets `PooledProvider` instead of shared `Adapter` | `parallel_runner.go` |
| `subSession` | Stays as-is (Go-level message isolation) | None |
| `OrchestratorFactory` | Creates sub-agent with pooled provider | `orchestrator.go` |
| New: `SlotPool` | Manages slot lifecycle | New file |
| New: `PooledProvider` | Adapter that acquires/releases from pool | New file |

### What Doesn't Change

- `AgentLoop` — doesn't know about slots, just calls `provider.StreamChat()`
- `Session` / `SessionTree` — message management is untouched
- Tool execution — completely separate concern
- SSE streaming — completely separate concern

### Kernel Compatibility

This follows the kernel pattern. The `AgentLoop` stays untouched. The slot pool is injected at the wiring layer. The loop just calls `StreamChat()` and doesn't know whether it's talking to a pooled slot, a dedicated slot, or a cloud provider.

---

## Sub-Agent Context Size Trade-off

Current: 16K for all sub-agents. This is generous for simple tasks (bash commands, file edits) but necessary for research (large scrape results).

Option: **Variable context size per role.**

```yaml
# config/roles/sarah/config.yaml
context_size: 16384  # research gets big context for scrape results

# config/roles/alex/config.yaml
context_size: 4096   # shell ops need minimal context

# config/roles/marcus/config.yaml
context_size: 8192   # code editing needs moderate context
```

This lets us fit more concurrent sub-agents by giving each only what it needs. The slot pool can check `ctxSize` before acquiring to make sure it fits the remaining VRAM budget.

---

## Decision Points

1. **Max sub-agent slots:** Start with 2 on 4090. Configurable via `config/models.json`.
2. **Pool exhaustion behavior:** Queue with timeout (30s), then fall back to cloud.
3. **CTO slot persistence:** CTO slot is never released. It stays warm for the entire session.
4. **Variable context per role:** Yes. Add `context_size` to role config.yaml.
5. **Slot health monitoring:** Check VRAM usage via `/props` endpoint. Refuse to acquire if VRAM is too tight.

---

## Priority

**This is higher priority than the brainstorming patterns.** Reason:

- `delegate_async` is broken on local (serial, not parallel)
- KV cache thrashing wastes GPU time on every sub-agent call
- The brainstorming patterns (hooks, middleware, events) all build on top of the execution model
- Fixing the foundation first means the patterns get built on solid ground

Implementation order:
1. `SlotPool` — bounded slot management
2. `PooledProvider` — acquires/releases from pool
3. Wire into `ParallelRunner` and `OrchestratorFactory`
4. Variable `ctxSize` per role
5. Cloud fallback on pool exhaustion
