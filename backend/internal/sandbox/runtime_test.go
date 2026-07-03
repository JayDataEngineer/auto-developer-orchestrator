package sandbox

import "testing"

// TestResolveRuntime covers the decision table for runtime selection.
// The pickRuntime method on Manager is a thin wrapper that supplies the
// env var + runsc-availability probe; this test exercises the pure
// decision function so we don't need a Docker daemon.
//
// Decision table (envValue, tier, runscAvailable) → runtime:
//   - any *, TierBridged, *          → "" (untested combo)
//   - "none", *, *                    → "" (explicit opt-out)
//   - "runsc", non-Bridged, *         → "runsc" (back-compat opt-in)
//   - "kata", non-Bridged, *          → "kata" (arbitrary override)
//   - "", TierIsolated, true          → "runsc" (new default-on)
//   - "", TierIsolated, false         → "" (runsc not installed)
//   - "", TierNative, *               → "" (TierNative doesn't get default-on)
func TestResolveRuntime(t *testing.T) {
	cases := []struct {
		name           string
		tier           SandboxTier
		envValue       string
		runscAvailable bool
		want           string
	}{
		{
			name:           "isolated + runsc installed + no env → runsc",
			tier:           TierIsolated,
			envValue:       "",
			runscAvailable: true,
			want:           "runsc",
		},
		{
			name:           "isolated + runsc NOT installed + no env → runc",
			tier:           TierIsolated,
			envValue:       "",
			runscAvailable: false,
			want:           "",
		},
		{
			name:           "bridged is always runc even with runsc installed",
			tier:           TierBridged,
			envValue:       "",
			runscAvailable: true,
			want:           "",
		},
		{
			name:           "bridged ignores explicit runsc opt-in",
			tier:           TierBridged,
			envValue:       "runsc",
			runscAvailable: true,
			want:           "",
		},
		{
			name:           "explicit opt-out via none",
			tier:           TierIsolated,
			envValue:       "none",
			runscAvailable: true,
			want:           "",
		},
		{
			name:           "explicit opt-in via runsc",
			tier:           TierIsolated,
			envValue:       "runsc",
			runscAvailable: false, // env beats probe
			want:           "runsc",
		},
		{
			name:           "arbitrary runtime override",
			tier:           TierIsolated,
			envValue:       "kata-runtime",
			runscAvailable: true,
			want:           "kata-runtime",
		},
		{
			name:           "TierNative doesn't get default-on",
			tier:           TierNative,
			envValue:       "",
			runscAvailable: true,
			want:           "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveRuntime(tc.tier, tc.envValue, tc.runscAvailable)
			if got != tc.want {
				t.Errorf("resolveRuntime(%q, %q, %v) = %q, want %q",
					tc.tier, tc.envValue, tc.runscAvailable, got, tc.want)
			}
		})
	}
}
