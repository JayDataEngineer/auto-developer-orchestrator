# Environment Abstraction Deep Analysis

Studied: `reference/hermes-agent/tools/environments/` (base.py, ssh.py, file_sync.py) and `agent/file_safety.py`

## 1. What Hermes-Agent Has (That We Don't)

### 1.1 BaseEnvironment ABC

`tools/environments/base.py` defines a class hierarchy for ALL execution backends:

```
BaseEnvironment (ABC)
├── LocalEnvironment     — subprocess on host
├── DockerEnvironment   — docker exec on container
├── SSHEnvironment      — ssh user@host bash -c
├── SingularityEnvironment
├── ModalEnvironment
├── DaytonaEnvironment
└── VercelEnvironment
```

The contract is four methods:

| Method | What it does |
|--------|-------------|
| `_run_bash(cmd, login, timeout, stdin)` | Spawn a shell process on the target |
| `cleanup()` | Tear down resources |
| `_before_execute()` | Hook before each command (file sync) |
| `execute(command, cwd, timeout, stdin)` → `{output, returncode}` | **Unified entry point** |

The base class provides all the shared machinery so backends only implement the transport layer:
- `init_session()` — captures login shell env vars into a snapshot file, sourced before every subsequent command
- `_wrap_command()` — wraps user command with: source snapshot → cd → eval → re-dump env → emit CWD marker
- `_wait_for_process()` — interrupt-aware poll loop with adaptive sleep, timeout, stdout drain via select()
- `_update_cwd()` — parses in-band CWD marker from output, strips it from result
- `__del__` → `cleanup()` fallback

### 1.2 SSHEnvironment (`tools/environments/ssh.py`)

Uses **SSH CLI subprocess** (not paramiko, not Go's `x/crypto/ssh`). Every command spawns a fresh `ssh user@host bash -c "..."`.

Key design decisions:

- **ControlMaster multiplexing** — one Unix domain socket per `user@host:port`. Socket path is SHA256-hashed to stay under macOS's 104-byte `sun_path` limit. Created on `__init__`, torn down on `cleanup()` via `ssh -O exit`.
- **ControlPersist=300** keeps the connection alive between commands (5min idle timeout).
- **`StrictHostKeyChecking=accept-new`** — auto-accepts new host keys, fails on mismatch.
- **`BatchMode=yes`** — never prompts interactively.
- **Tar-over-SSH pipe for bulk upload** — `tar c` piped through SSH to `remote tar x`. ~580 files in one TCP stream vs. N scp round-trips. Uses symlink staging in a temp dir to avoid fragile GNU tar `--transform` rules. `--no-overwrite-dir` prevents breaking sshd StrictModes.
- **Bulk download** — `ssh tar cf -` piped to local file.
- **Sync-back** — on teardown, SSH tar-extracts remote `.hermes/`, SHA-256 diffs against what was pushed, only applies files that changed. Retries with exponential backoff. SIGINT-deferred during sync. File-lock serialized across concurrent gateway processes.

### 1.3 FileSyncManager (`tools/environments/file_sync.py`)

Change-tracked, rate-limited, transactional file sync for remote backends (SSH, Modal, Daytona — not for Docker/Singularity which use bind mounts).

- Tracks `(mtime, size)` per remote path — only uploads what changed
- Detects deletions — files removed locally → deleted remotely
- Transactional: state only commits on full success, rolls back on any failure
- Rate-limited to once per `_SYNC_INTERVAL_SECONDS` (5s)
- `sync_back()` — SHA-256 based diff on teardown, file-lock serialized, SIGINT-deferred
- 2 GiB safety cap on tar extraction
- Conflict detection: if host file was modified since push AND remote also changed, applies remote (last-write-wins)

### 1.4 SSH Path Write Protection (`agent/file_safety.py`)

Exact-path deny list:

```
~/.ssh/authorized_keys
~/.ssh/id_rsa
~/.ssh/id_ed25519
~/.ssh/config
~/.bashrc, .zshrc, .profile, .bash_profile, .zprofile
~/.netrc, .pgpass, .npmrc, .pypirc, .git-credentials
/etc/sudoers, /etc/passwd, /etc/shadow
```

Prefix-level deny list (recursive):

```
~/.ssh/
~/.aws/
~/.gnupg/
~/.kube/
/etc/sudoers.d/
/etc/systemd/
~/.docker/
~/.azure/
~/.config/gh/
~/.config/gcloud/
```

And threat pattern scanning (`tools/threat_patterns.py`, `tools/skills_guard.py`):
- `authorized_keys` → `"ssh_backdoor"` (critical severity, persistence category)
- `ssh-keygen` → `"ssh_keygen"` (medium severity, persistence category)

Plus cross-profile write guards — prevents agent under profile A from writing to profile B's skills/plugins/cron/memories.

---

## 2. What We Currently Do

### 2.1 SSH (Go, `go-backend/internal/ssh/session.go`)

- Go-native `x/crypto/ssh` — proper library-level SSH, not CLI subprocess
- `SessionManager` with `sync.Map` connection pool keyed by `user@host:port`
- Auth priority: explicit key → SSH agent → auto-loaded `~/.ssh/id_*` → password
- Host key: auto-accepts and stores to `~/.pux/ssh/known_hosts`
- Session tracking via random session keys
- SFTP-based filesystem (`SshFS` implementing `ProjectFS`)
- WebSocket interactive terminal (`terminal_ssh.go`)

### 2.2 File Ops

- Per-file SFTP for all read/write/move/delete
- No bulk sync, no change tracking, no sync-back
- No tar pipe streaming

### 2.3 Security

- No write protection for SSH paths
- No threat pattern scanning
- No cross-profile guards

---

## 3. What We Should Steal

### 🔴 P0 — Implement Now

#### 3.1 Tar-over-SSH Bulk Transfer

**Problem:** Every sandbox setup uploads ~hundreds of files one-by-one via SFTP. This is 10-50x slower than necessary.

**Solution:** Add a `BulkUpload(bulkUploadFn)` pattern. Stream `tar c` → SSH → `tar x` in one TCP connection.

```go
// Pseudocode for the tar-over-SSH approach
func BulkUpload(ctx context.Context, client *ssh.Client, files []FilePair) error {
    // 1. Create staging dir with symlinks
    // 2. tar c -C staging . | ssh remote tar xf - -C /remote/base
    // 3. Use --no-overwrite-dir to protect sshd StrictModes
}
```

**Files to create/modify:**
- New: `go-backend/internal/ssh/bulk.go` — tar pipe streaming
- Modify: `go-backend/internal/sandbox/` — use for sandbox provisioning

#### 3.2 SSH Path Write Denial

**Problem:** Nothing stops the agent from writing to `~/.ssh/authorized_keys`, `~/.ssh/id_rsa`, or `~/.ssh/config`. This is a trivial persistence vector.

**Solution:** Add a deny list in our file operation handlers (`pux_files.go`, `handlers/pux_files.go`).

```go
var sshDeniedExactPaths = []string{
    ".ssh/authorized_keys",
    ".ssh/id_rsa",
    ".ssh/id_ed25519",
    ".ssh/config",
}
var sshDeniedPrefixes = []string{
    ".ssh/",
}
```

**Files to modify:**
- `go-backend/internal/handlers/pux_files.go` — add `isSSHPathDenied()` check before write/create/delete
- `go-backend/internal/llama/` — check in shell commands too

### 🟡 P1 — Next Priority

#### 3.3 FileSyncManager (Change Tracking)

**Problem:** Every sync cycle uploads everything, even unchanged files. No deletion detection.

**Solution:** Track `(mtime, size)` per remote path. Only upload what changed. Detect and propagate deletions.

**Implementation approach:**
- Add `SyncedFiles` map to the sandbox lifecycle
- Rate-limit syncs to once per 5s
- Transactional state — roll back on failure
- Apply to SSH environments AND the sandbox Docker setup

**Files to create/modify:**
- New: `go-backend/internal/ssh/filesync.go` — FileSyncManager in Go
- Modify: sandbox lifecycle to use it

#### 3.4 Sync-Back on Teardown

**Problem:** Remote changes (agent-created files) are lost when sandbox/SSH session ends.

**Solution:** On `cleanup()`, pull remote changes via tar, SHA-256 diff against pushed state, apply only what changed.

**Files to modify:**
- `go-backend/internal/sandbox/` — add sync-back to sandbox teardown
- `go-backend/internal/ssh/session.go` — add sync-back to SSH disconnect

#### 3.5 Threat Pattern Scanning

**Problem:** No detection when agent writes `authorized_keys` or runs `ssh-keygen`.

**Solution:** Regex-based scanning of tool call args and shell command strings.

```go
var sshThreatPatterns = []struct{
    pattern *regexp.Regexp
    severity string
    category string
} {
    {regexp.MustCompile(`authorized_keys`), "critical", "persistence"},
    {regexp.MustCompile(`ssh-keygen`), "medium", "persistence"},
}
```

**Files to modify:**
- `go-backend/internal/llama/` — add to tool-call validation pipeline

### 🟢 P2 — Nice to Have

#### 3.6 BaseEnvironment Class (in Go)

Port the abstract `BaseEnvironment` → concrete backend pattern:

```go
type Environment interface {
    Execute(ctx context.Context, command string, opts ExecuteOptions) (ExecuteResult, error)
    Cleanup() error
    Sync(ctx context.Context) error
}
```

With implementations:
- `LocalEnvironment` — `os/exec`
- `SSHEnvironment` — `x/crypto/ssh`
- `DockerEnvironment` — Docker API
- `SandboxEnvironment` — our Docker sandbox

This would give us:
- Single `execute()` entry point for all environments
- Common `_wrapCommand()` → source env → cd → eval → emit markers
- Common `_waitForProcess()` → interrupt handling, timeout, stdout drain
- Pluggable backends with zero agent loop changes

#### 3.7 Session Snapshots (Env/CWD Persistence)

**Problem:** Each SSH command starts with a clean shell. Environment variables set by a previous command are lost.

**Solution:** Capture login shell env vars into a snapshot file on init. Source before every subsequent command. Track CWD via in-band stdout markers.

#### 3.8 ControlMaster Connection Improvements

Our Go approach already pools connections, but we could add:
- Keepalive (equivalent to `ControlPersist=300`)
- Connection health checks (auto-reconnect on broken pipe)
- Socket path hashing for macOS compatibility

---

## 4. Architecture Comparison

```
┌─────────────────────────────────────────────────────────────────┐
│ HERMES-AGENT (Python)                                          │
│                                                                 │
│  terminal_tool.py                                               │
│    └─ create_environment() ── factory                          │
│         ├─ LocalEnvironment   ── subprocess                     │
│         ├─ DockerEnvironment  ── docker exec                    │
│         ├─ SSHEnvironment     ── ssh user@host bash -c          │
│         ├─ ModalEnvironment   ── SDK                            │
│         └─ DaytonaEnvironment ── SDK                            │
│                                                                 │
│  All share:                                                     │
│    BaseEnvironment.execute(command, cwd) → {output, returncode} │
│    BaseEnvironment._wrap_command()                               │
│    BaseEnvironment._wait_for_process()                           │
│    FileSyncManager.sync() / sync_back()                          │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│ OUR SYSTEM (Go)                                                 │
│                                                                 │
│  No environment abstraction. Each backend is ad-hoc:            │
│    - llama/agent.go ── direct tool calls                       │
│    - handlers/ssh_browse.go ── SFTP for file browser           │
│    - handlers/terminal_ssh.go ── WebSocket SSH terminal         │
│    - sandbox/ ── Docker containers                              │
│    - ssh/session.go ── connection pool                          │
│                                                                 │
│  No FileSyncManager, no bulk transfer, no sync-back.            │
│  No SSH path protection.                                        │
└─────────────────────────────────────────────────────────────────┘
```

The Hermes pattern is more work upfront but pays off when:
- Adding a new backend (just implement 4 methods)
- Changing execution semantics (one place: `BaseEnvironment`)
- File sync (plug `FileSyncManager` into `_before_execute`/`cleanup`)

Our Go approach is pragmatic for the current surface area (SSH browsing + terminal), but lacks the bulk sync and security layers we need for sandbox-based agents.

---

## 5. Key Code References

| File | Lines | What to study |
|------|-------|---------------|
| `reference/hermes-agent/tools/environments/base.py` | 288-854 | BaseEnvironment ABC, execute(), wrap_command(), wait_for_process() |
| `reference/hermes-agent/tools/environments/ssh.py` | 36-308 | SSHEnvironment: ControlMaster, tar pipe, bulk ops, cleanup |
| `reference/hermes-agent/tools/environments/file_sync.py` | 107-402 | FileSyncManager: change tracking, sync-back, SHA-256 diff |
| `reference/hermes-agent/agent/file_safety.py` | 28-83 | `build_write_denied_paths()`, `build_write_denied_prefixes()` |
| `reference/hermes-agent/tools/threat_patterns.py` | 107-108 | `authorized_keys`, `ssh-keygen` patterns |
| `reference/hermes-agent/tools/skills_guard.py` | 234-239 | SSH backdoor/keygen skill guards |
| `go-backend/internal/ssh/session.go` | 1-332 | Our current SSH session manager |
| `go-backend/internal/handlers/pux_files.go` | 1-283 | Our file operations (no path protection) |
| `go-backend/internal/sandbox/` | — | Sandbox lifecycle (no bulk sync, no sync-back) |
| `reference/hermes-agent/.env.example` | 202-216 | SSH env var config documentation |
| `reference/hermes-agent/tests/tools/test_ssh_environment.py` | — | SSH test patterns |
| `reference/hermes-agent/tests/tools/test_ssh_bulk_upload.py` | — | Bulk upload test patterns (545 lines) |
| `reference/hermes-agent/tests/tools/test_sync_back_backends.py` | — | Sync-back test patterns |

---

## 6. Implementation Order

```
Phase 1 (This sprint)
├── SSH path write denial ──~/.ssh/authorized_keys, id_rsa, config
├── Threat pattern scanning ──authorized_keys/ssh-keygen in tool calls
└── Tar-over-SSH bulk upload ──replace per-file SFTP for sandbox setup

Phase 2 (Next sprint)
├── FileSyncManager ──mtime tracking, change-only sync, rate limiting
├── Sync-back on teardown ──pull + SHA-256 diff
└── BaseEnvironment abstraction ──Go interface for all backends

Phase 3 (Future)
├── Session snapshots ──env/cwd persistence across commands
├── Connection health checks ──ControlMaster-equivalent keepalive
└── Cross-profile write guards
```
