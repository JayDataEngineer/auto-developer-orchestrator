package sandbox

import (
	"context"
	"os"

	"github.com/moby/moby/client"
)

// pickRuntime decides which Docker runtime (runc, runsc, etc.) a sandbox
// container should use. Returns the runtime name (e.g. "runsc") or "" for
// Docker's default (runc). Thin wrapper around resolveRuntime that supplies
// the env var + runs the runsc availability probe.
//
// See resolveRuntime for the full decision table.
func (m *Manager) pickRuntime(ctx context.Context, tier SandboxTier) string {
	return resolveRuntime(tier, os.Getenv("PUX_SANDBOX_RUNTIME"), m.isRunscAvailable(ctx))
}

// resolveRuntime is the pure decision function for runtime selection.
// Extracted so it has unit tests that don't need a Docker daemon.
//
// Resolution order:
//
//  1. TierBridged sandboxes always return "" — runsc + NET_HOST + Xvfb is
//     an untested combination we don't want to surprise operators with.
//  2. envValue (PUX_SANDBOX_RUNTIME) when set:
//     - "none" → explicit opt-out, return ""
//     - any other value → that value (back-compat with the existing
//       `PUX_SANDBOX_RUNTIME=runsc` opt-in; also lets operators pick
//       arbitrary runtimes like kata-runtime)
//  3. When env is unset AND tier is TierIsolated AND runscAvailable →
//     return "runsc". This is the default-on behavior: kernel-level
//     syscall interception for every isolated sandbox without operators
//     needing to opt in.
//  4. Otherwise → "" (Docker default, runc).
func resolveRuntime(tier SandboxTier, envValue string, runscAvailable bool) string {
	if tier == TierBridged {
		return ""
	}

	switch envValue {
	case "none":
		return ""
	case "":
		if tier == TierIsolated && runscAvailable {
			return "runsc"
		}
		return ""
	default:
		return envValue
	}
}

// isRunscAvailable queries the Docker daemon for registered runtimes.
// Returns false on any error (Docker down, client nil, etc.) — failing
// closed to runc is safer than failing closed to "no sandbox create."
//
// The probe hits `docker info` once per call (~50ms). Cheap relative to
// container create, no caching needed.
func (m *Manager) isRunscAvailable(ctx context.Context) bool {
	if m.dockerClient == nil {
		return false
	}
	resp, err := m.dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return false
	}
	_, ok := resp.Info.Runtimes["runsc"]
	return ok
}
