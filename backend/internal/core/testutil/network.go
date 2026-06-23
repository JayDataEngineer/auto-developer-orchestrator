package testutil

import (
	"net"
	"os"
	"testing"
	"time"
)

// listenLoopback binds an ephemeral TCP port on the loopback interface. Used
// by tests that need a real listener to probe.
func listenLoopback() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// Network skips the calling test unless PUX_RUN_NETWORK_TESTS=1 is set in the
// environment.
//
// Use this guard for any test that hits the real internet, opens a real port,
// or depends on a live external service (MCP servers, llama-server, the
// cluster, browsers, Docker sandboxes). It keeps `go test ./...` hermetic by
// default while still letting developers opt in to the integration path:
//
//	PUX_RUN_NETWORK_TESTS=1 go test ./internal/tools/decltools/...
//
// The same env var is also used by the Python test suite
// (tests/python/conftest.py) — keep the name in sync.
func Network(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("PUX_RUN_NETWORK_TESTS") == "" {
		t.Skipf("skipping network/integration test: %s — set PUX_RUN_NETWORK_TESTS=1 to enable", reason)
	}
}

// NetworkOrService skips the calling test unless PUX_RUN_NETWORK_TESTS=1 OR
// the named service is reachable at the given address.
//
// Use this for tests that should run when either knob is enabled — common
// for tests that hit a local service in dev (where the probe succeeds) but
// should auto-skip in CI (where neither knob is on).
//
//	address accepts "host:port" or "http://host:port" form.
func NetworkOrService(t *testing.T, reason, address string) {
	t.Helper()
	if os.Getenv("PUX_RUN_NETWORK_TESTS") != "" {
		return
	}
	if !serviceReachable(address) {
		t.Skipf("skipping integration test: %s — service %s not reachable and PUX_RUN_NETWORK_TESTS not set", reason, address)
	}
}

// serviceReachable does a TCP connect probe with a short timeout. Returns
// false on any error — callers treat unreachable the same as "not configured".
func serviceReachable(address string) bool {
	host, port := splitHostPort(address)
	if host == "" || port == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// splitHostPort accepts "host:port" or "scheme://host:port/path?query" and
// returns ("", "") for anything that doesn't parse cleanly.
func splitHostPort(s string) (host, port string) {
	// Strip scheme if present.
	for i := 0; i+3 <= len(s); i++ {
		if s[i] == ':' && i+2 < len(s) && s[i+1] == '/' && s[i+2] == '/' {
			s = s[i+3:]
			break
		}
	}
	// Strip path if present.
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			s = s[:i]
			break
		}
	}
	// Strip userinfo if present.
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			s = s[i+1:]
			break
		}
	}
	// Now s is "host:port" or "host".
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return s, ""
	}
	return host, port
}
