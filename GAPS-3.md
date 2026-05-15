# GAPS-3: Subagent Flow & Tool Usage Audit

Date: 2026-05-15

## Issues Found & Fixed

### Gap 1: `delegate_continue` never works — `liveAgents` never populated [FIXED]

**File:** `go-backend/internal/tools/orchestration/parallel_runner.go`

`RunDelegateTracked()` used to call `RunDelegate()` which defer-closed the provider. Now it runs the delegate loop directly, keeps the provider alive, and stores the agent in `liveAgents`. `delegate_continue` can now send feedback to completed delegates.

**Fix:** Refactored `RunDelegateTracked` to run the delegate loop inline (not via `RunDelegate`), store in `liveAgents` on success, close provider on error. Added `ParallelRunner.Close()` to clean up live agents on session end. Wired into `orchestrator.Agent.Close()`.

---

### Gap 2: `delegate_async` ignores role model, max_rounds, temperature [FIXED]

**File:** `go-backend/internal/tools/orchestration/orchestration.go`, `parallel_runner.go`

Async delegates hardcoded `15, 0.4, "", ""` ignoring role-specific config.

**Fix:** Updated `DelegateRunner.RunDelegateAsync` signature to accept `maxRounds`, `temperature`, `modelID`. `DelegateAsyncTool.Execute()` now passes resolved role values through.

---

### Gap 3: `delegate_async` has no file change tracking [FIXED]

**File:** `go-backend/internal/tools/orchestration/parallel_runner.go`

Async delegates now call `RunDelegateTracked` instead of `RunDelegate`, giving them snapshots, diffs, and agent_refs. After completion, the live agent is cleaned up (provider closed) since async delegates don't support continuation.

**Fix:** `RunDelegateAsync` calls `RunDelegateTracked`, then immediately removes from `liveAgents` and closes provider.

---

### Gap 4: `browser` capability only lists `bash` [FIXED]

**File:** `config/capabilities/browser/capability.yaml`

Added all Go-native browser tools registered by `RegisterBrowserTools()`: `find_element`, `snapshot_a11y`, `get_cookies`, `set_cookie`, `clear_cookies`, `get_storage`, `set_storage`, `clear_storage`.

---

### Gap 5: Sub-agent compaction is lossy [DEFERRED]

**File:** `go-backend/internal/tools/orchestration/parallel_runner.go`

`subSession.Compact()` drops old tool results and truncates messages to 300 chars. Acceptable for short-lived sub-agents. Now that `delegate_continue` works, this may need improvement if continuations become long.

**Severity:** Low — deferred until real-world usage shows it's a problem.

---

### Gap 6: Async delegates bypass depth guard [FIXED]

**File:** `go-backend/internal/tools/orchestration/parallel_runner.go`

**Fix:** Added `r.depth >= r.maxDepth` check at the top of `RunDelegateAsync`.

---

## What Works Well

- Provider isolation — each sub-agent gets fresh session slot
- Event forwarding — sub-agent tool/text/thinking events correctly forwarded with AgentName
- Auto-director — Chrome raised when browser tools detected
- Per-tier executor factory — `native` gets HostExecutor, others use sandbox
- Vision caching — VisionAwareExecutor wraps sub-agent executors
- Tool call deduplication — handles DeepSeek-style duplicates
- Git snapshots — pre-snapshot + diff for file change tracking
- Error classification — transient vs permanent with appropriate retry
- Role resolution — org first, then kernel, with MCP expansion
- Capability overlay — new capabilities override legacy tool_packages
