# GAPS-2 — Sandbox Hardening + Lifecycle

Tracker for the security + lifecycle gaps surfaced after the
pi-dynamic-workflows spike. Each gap has: why it matters, the fix,
files touched, and a verify gate.

Status legend: ⬜ todo · 🟡 in progress · ✅ done · ⏭ deferred

---

## Gap 1 — Resource limits default ✅

**Problem.** `cmd/mcpserver/main.go:157` constructs `SandboxOptions`
with no `MemoryLimit` or `CPULimit`. The manager honors them when set
(`manager.go:255-259`) but no caller sets them. An agent going wild
(fork bomb, infinite loop, OOM bait) has the whole host to chew on.

**Fix (shipped).**
- New `backend/internal/sandbox/defaults.go` with `resolveResourceDefaults`:
  applies env-overridable defaults when caller passes zero values.
- Defaults: `PUX_SANDBOX_MEMORY_MB=2048`, `PUX_SANDBOX_CPU_CORES=2.0`,
  `PUX_SANDBOX_PIDS=512`. Caller-supplied non-zero values win.
- `manager.go::CreateSandbox` wires the helper into `container.Resources`.
- `PidsLimit` always set from env (no caller field — no legit reason for
  agent sandboxes to need unlimited processes).

**Files.**
- `backend/internal/sandbox/defaults.go` (NEW) — helper + env parsers.
- `backend/internal/sandbox/manager.go` — replaced bare resource struct
  with call to `resolveResourceDefaults`.

**Verify (passed).**
- `backend/internal/sandbox/defaults_test.go` — 7-case table test for
  resolution rules + env parsers.
- `docker inspect` on live container:
  - defaults: `Memory=2147483648 NanoCPUs=2000000000 PidsLimit=512`
  - env override (`PUX_SANDBOX_MEMORY_MB=4096 PUX_SANDBOX_CPU_CORES=4
    PUX_SANDBOX_PIDS=1024`): `Memory=4294967296 NanoCPUs=4000000000
    PidsLimit=1024` ✓

---

## Gap 2 — gVisor on by default for TierIsolated ✅

**Problem.** gVisor (runsc) opt-in shipped 2026-07-01 via
`PUX_SANDBOX_RUNTIME=runsc` (`manager.go:290-299`). Off by default
meant most sandboxes today are just Docker containers with Docker's
default seccomp profile — no kernel-level syscall filtering.

**Fix (shipped).**
- New `backend/internal/sandbox/runtime.go` with:
  - `pickRuntime(ctx, tier)` — Manager method, thin wrapper around
    `resolveRuntime` that supplies env + probes Docker.
  - `resolveRuntime(tier, envValue, runscAvailable)` — pure decision
    function. TierBridged → "". env="none" → "". env=other → that value.
    env unset + TierIsolated + runsc installed → "runsc". Else → "".
  - `isRunscAvailable(ctx)` — calls `docker info`, checks `Runtimes["runsc"]`.
- `manager.go::CreateSandbox` uses `m.pickRuntime(ctx, resolvedTier)`
  in both the initial gVisor block AND the policy-override re-eval.
- Backwards-compat: existing `PUX_SANDBOX_RUNTIME=runsc` still works
  as explicit opt-in. New `=none` lets operators opt out even when
  runsc is installed.

**Files.**
- `backend/internal/sandbox/runtime.go` (NEW).
- `backend/internal/sandbox/manager.go` — replaced env-only checks with
  `pickRuntime` calls.

**Verify (passed).**
- `backend/internal/sandbox/runtime_test.go` — 8-case table test for
  the decision function.
- `docker inspect` on this daemon (no runsc installed): `Runtime=runc`
  — probe correctly returned false, fell through to default.
- On a daemon with runsc installed: would resolve to `Runtime=runsc`.

---

## Gap 3 — PID limit ✅

**Problem.** Fork bombs weren't contained — they'd hit the host
OOM-killer via memory pressure, not a per-container PID cap.

**Fix (shipped).** Folded into Gap 1. Default `PidsLimit=512`,
env-overridable via `PUX_SANDBOX_PIDS`.

---

## Gap 4 — Tier normalization ✅

**Problem.** Callers passing empty `opts.Tier` got the Go zero value
`""`, which didn't match `TierIsolated` in downstream branches
(`resolvedTier != TierBridged` for egress staging, the gVisor default,
the bridged-mount branch). Inconsistent behavior between callers that
passed `Tier: TierIsolated` explicitly vs. those that left it unset.

**Fix (shipped).**
- `manager.go::CreateSandbox` — one-line normalization at the top:
  `if opts.Tier == "" { opts.Tier = TierIsolated }`.
- Now every downstream branch treats "no tier set" identically to
  "tier: isolated". The contract is documented in code: TierIsolated
  without `policy.yaml` = no egress ACL (today's behavior, made
  explicit). Operators wanting a firewall opt in via policy.yaml.

**Files.**
- `backend/internal/sandbox/manager.go` — 5-line block at top of
  `CreateSandbox` (after ID generation).

**Verify (passed).** `task smoke` clean against real Docker with the
default-tier code path. Full Go suite passes under -race.

---

## Gap 5 — DNS resolved once at boot ✅

**Problem.** Egress ACL resolved hostnames to IPs once at sandbox
create (`policy.go::EgressRules` calls `net.LookupHost`). If a CDN
rotated IPs mid-session, the firewall kept allowing the old IP and
dropping the new one — agent saw mysterious timeouts hours into a
session.

**Fix (shipped).**
- `backend/internal/policy/egress.go::EgressRules` now emits a
  `# host: <name>` comment line BEFORE the IP lines for each DNS-resolved
  host. Literal IP entries get no comment (nothing to re-resolve).
  Comments are skipped by `apply-egress-policy.sh` (the boot script),
  so the format is backwards-compatible.
- New `sandbox/scripts/refresh-egress-dns.sh`:
  - Parses egress.conf into per-host blocks via the `# host:` markers.
  - For each DNS host: re-resolves via `getent hosts`, emits fresh IP
    lines for current IPs × deduped ports.
  - Literal-IP entries (no preceding `# host:`) copied verbatim.
  - On DNS failure: keeps old IPs for that host (no surprise firewall
    closure on transient DNS outage).
  - Atomically replaces egress.conf + calls apply-egress-policy.sh
    to flush + re-add.
- New `sandbox/scripts/egress-dns-refresh-loop.sh` — sleep 5min +
  refresh, forever. Period overridable via `PUX_EGRESS_REFRESH_INTERVAL`.
- New supervisor program `egress-dns-refresh` at priority 16 (after
  firewall at 15) — supervisor-managed alternative to cron, no new dep.
- Dockerfile copies + chmods both new scripts.

**Files.**
- `backend/internal/policy/egress.go` — emit `# host:` comments for
  DNS hosts.
- `backend/internal/policy/egress_test.go` (NEW) — 2 tests for the
  hostname-comment contract.
- `sandbox/scripts/refresh-egress-dns.sh` (NEW).
- `sandbox/scripts/egress-dns-refresh-loop.sh` (NEW).
- `sandbox/supervisord.conf` — added `[program:egress-dns-refresh]`.
- `sandbox/Dockerfile` — COPY + chmod both scripts.

**Verify (passed).**
- `go test -race ./internal/policy/...` — clean.
- Real-container test of `refresh-egress-dns.sh` against a fake conf
  with all three paths: literal IP preserved verbatim, failed DNS
  (`this-host.invalid`) kept old IPs with WARN log, working DNS
  (`example.com`) re-resolved from `1.2.3.4` to current Cloudflare IPs.
- Real-container test of `apply-egress-policy.sh` with new conf format:
  `# host:` comments correctly skipped, IP lines applied to iptables.
- `task smoke` clean with new scripts in the image.

---

## Gap 6 — Per-session sandbox lifecycle ⏭

**Problem.** Every pux invocation boots a fresh Docker container
(~3s boot tax). For CI/batch dispatch workloads, this adds up.

**Status.** Deferred. Real architectural change (sandbox keyed by
workspace path, idle timeout, hot reload on image change). Deserves
its own design pass. Tracking here so it doesn't get forgotten.

---

## Implementation order (final)

1. ✅ Gap 1 + Gap 3 (resource limits + PIDs) — `defaults.go` + tests.
2. ✅ Gap 4 (tier normalization) — one-liner in `manager.go`.
3. ✅ Gap 2 (gVisor default) — `runtime.go` + tests.
4. ✅ Gap 5 (DNS refresh) — egress.go + 2 scripts + supervisor program.
5. ⏭ Gap 6 — deferred.

## Verify gates (overall, all passed)

- ✅ `go test -race -count=1 ./...` — full Go suite, no regressions
  (15 packages, ~30s).
- ✅ `task smoke` — real-Docker smoke test passes with new defaults +
  scripts (every tool round-trips).
- ✅ `docker inspect` on live sandbox — Memory=2GB, NanoCPUs=2 cores,
  PidsLimit=512, Runtime="runc" (runsc not installed on this daemon,
  fallback correct). Env overrides proven: PUX_SANDBOX_MEMORY_MB,
  PUX_SANDBOX_CPU_CORES, PUX_SANDBOX_PIDS.
- ⏭ Boot twitter-agent sandbox — regression check deferred (cookies
  + egress still work in unit tests; live twitter flow needs API
  access).
