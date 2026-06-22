package manifest

import "testing"

// TestSandboxConfigValidate_SurrealDBEnvGroup verifies the cross-field env
// invariant: SURREALDB_URL/NS/DB must be set as a group. Catches the typo
// "SURREALDB_NS missing" that silently breaks every surreal_client.py call.
func TestSandboxConfigValidate_SurrealDBEnvGroup(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want int // expected number of errors
	}{
		{
			name: "all three set",
			env: map[string]string{
				"SURREALDB_URL": "http://x",
				"SURREALDB_NS":  "a",
				"SURREALDB_DB":  "b",
			},
			want: 0,
		},
		{
			name: "URL missing",
			env: map[string]string{
				"SURREALDB_NS": "a",
				"SURREALDB_DB": "b",
			},
			want: 1,
		},
		{
			name: "NS missing",
			env: map[string]string{
				"SURREALDB_URL": "http://x",
				"SURREALDB_DB":  "b",
			},
			want: 1,
		},
		{
			name: "DB missing",
			env: map[string]string{
				"SURREALDB_URL": "http://x",
				"SURREALDB_NS":  "a",
			},
			want: 1,
		},
		{
			name: "none set — no error (validator only fires when at least one is set)",
			env:  map[string]string{},
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &SandboxConfig{Env: c.env}
			got := len(s.Validate())
			if got != c.want {
				t.Errorf("got %d errors, want %d. errs=%v", got, c.want, s.Validate())
			}
		})
	}
}

// TestSandboxConfigValidate_NilSandbox ensures a nil receiver is safe —
// LoadManifest can return a PuxManifest with Sandbox == nil, and callers
// may still invoke Validate().
func TestSandboxConfigValidate_NilSandbox(t *testing.T) {
	var s *SandboxConfig
	if errs := s.Validate(); errs != nil {
		t.Errorf("nil SandboxConfig should return nil errors, got %v", errs)
	}
}

// TestSandboxConfigValidate_UnrelatedEnvIsQuiet confirms unrelated env keys
// don't trip the SurrealDB check.
func TestSandboxConfigValidate_UnrelatedEnvIsQuiet(t *testing.T) {
	s := &SandboxConfig{Env: map[string]string{
		"MCP_HUB_ENDPOINT": "http://100.86.69.57:30080",
		"PUX_ORG_PATH":     "/sandbox/workspace",
	}}
	if errs := s.Validate(); len(errs) != 0 {
		t.Errorf("unrelated env should not produce errors, got: %v", errs)
	}
}
