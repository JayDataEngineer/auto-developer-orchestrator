package testutil

import (
	"os"
	"testing"
)

// runInSubtest runs fn inside a subtest of t and reports whether the subtest
// was skipped. We need a real *testing.T to call Skip on (SkipNow calls
// runtime.Goexit, which doesn't work on a manually-constructed &testing.T{}).
//
// fn is expected to call Skip itself; we wrap the assignment in defer so the
// SkipNow short-circuit still leaves the boolean set.
func runInSubtest(t *testing.T, fn func(st *testing.T)) bool {
	t.Helper()
	skipped := false
	t.Run("subtest", func(st *testing.T) {
		defer func() { skipped = st.Skipped() }()
		fn(st)
	})
	return skipped
}

// TestNetworkSkipsByDefault verifies Network() skips when
// PUX_RUN_NETWORK_TESTS is unset.
func TestNetworkSkipsByDefault(t *testing.T) {
	old := os.Getenv("PUX_RUN_NETWORK_TESTS")
	os.Unsetenv("PUX_RUN_NETWORK_TESTS")
	defer os.Setenv("PUX_RUN_NETWORK_TESTS", old)

	skipped := runInSubtest(t, func(st *testing.T) {
		Network(st, "test reason")
	})
	if !skipped {
		t.Fatal("expected Network() to skip when env var unset")
	}
}

// TestNetworkRunsWhenEnabled verifies Network() proceeds when
// PUX_RUN_NETWORK_TESTS=1.
func TestNetworkRunsWhenEnabled(t *testing.T) {
	old := os.Getenv("PUX_RUN_NETWORK_TESTS")
	os.Setenv("PUX_RUN_NETWORK_TESTS", "1")
	defer os.Setenv("PUX_RUN_NETWORK_TESTS", old)

	skipped := runInSubtest(t, func(st *testing.T) {
		Network(st, "test reason")
	})
	if skipped {
		t.Fatal("expected Network() to NOT skip when env var set")
	}
}

// TestNetworkOrServiceFallsThroughOnEnv verifies NetworkOrService() proceeds
// when PUX_RUN_NETWORK_TESTS=1, regardless of the service URL.
func TestNetworkOrServiceFallsThroughOnEnv(t *testing.T) {
	old := os.Getenv("PUX_RUN_NETWORK_TESTS")
	os.Setenv("PUX_RUN_NETWORK_TESTS", "1")
	defer os.Setenv("PUX_RUN_NETWORK_TESTS", old)

	skipped := runInSubtest(t, func(st *testing.T) {
		NetworkOrService(st, "reason", "definitely-not-real.invalid:9999")
	})
	if skipped {
		t.Fatal("expected NetworkOrService() to proceed when env var set")
	}
}

// TestNetworkOrServiceSkipsWhenUnreachable verifies NetworkOrService() skips
// when neither env var is set nor the service is reachable.
func TestNetworkOrServiceSkipsWhenUnreachable(t *testing.T) {
	old := os.Getenv("PUX_RUN_NETWORK_TESTS")
	os.Unsetenv("PUX_RUN_NETWORK_TESTS")
	defer os.Setenv("PUX_RUN_NETWORK_TESTS", old)

	skipped := runInSubtest(t, func(st *testing.T) {
		NetworkOrService(st, "reason", "definitely-not-real.invalid:9999")
	})
	if !skipped {
		t.Fatal("expected NetworkOrService() to skip when service unreachable")
	}
}

// TestSplitHostPort verifies URL parsing for various input forms.
func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantPort string
	}{
		{"localhost:8001", "localhost", "8001"},
		{"http://localhost:8001", "localhost", "8001"},
		{"https://api.example.com:443/v1/chat", "api.example.com", "443"},
		{"user:pass@host:5432", "host", "5432"},
		{"host", "host", ""},                  // no port
		{"", "", ""},                          // empty
		{"http://host/path", "host", ""},      // URL without port
	}
	for _, c := range cases {
		gotHost, gotPort := splitHostPort(c.in)
		if gotHost != c.wantHost || gotPort != c.wantPort {
			t.Errorf("splitHostPort(%q) = (%q, %q), want (%q, %q)",
				c.in, gotHost, gotPort, c.wantHost, c.wantPort)
		}
	}
}

// TestServiceReachableLoopback verifies a real listener is detected as
// reachable.
func TestServiceReachableLoopback(t *testing.T) {
	ln, err := listenLoopback()
	if err != nil {
		t.Skipf("could not bind loopback listener: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	if !serviceReachable(addr) {
		t.Errorf("serviceReachable(%q) = false, want true", addr)
	}
	// 127.0.0.1:1 is almost always refused — sanity check the negative path.
	if serviceReachable("127.0.0.1:1") {
		t.Errorf("serviceReachable(127.0.0.1:1) = true; want false")
	}
}
