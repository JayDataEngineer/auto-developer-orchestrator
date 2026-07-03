package sandbox

import (
	"os"
	"testing"
)

// TestResolveResourceDefaults exercises the env-overridable default
// resolution for MemoryLimit / CPULimit / PidsLimit. Caller-supplied
// non-zero values win; zero values fall back to env, then to constants.
// PidsLimit has no caller field — it's always env-or-default.
//
// These rules are enforced in production by CreateSandbox calling this
// helper. The defaults matter for security: an agent going wild (fork
// bomb, OOM bait) without these gets the whole host to chew on.
func TestResolveResourceDefaults(t *testing.T) {
	cases := []struct {
		name       string
		opts       SandboxOptions
		envMem     string
		envCPU     string
		envPids    string
		wantMem    int
		wantCPU    float64
		wantPids   int
	}{
		{
			name:     "all zero → constants",
			opts:     SandboxOptions{},
			wantMem:  defaultMemoryMB,
			wantCPU:  defaultCPUCores,
			wantPids: defaultPidsLimit,
		},
		{
			name:     "caller wins over constants",
			opts:     SandboxOptions{MemoryLimit: 4096, CPULimit: 4.0},
			wantMem:  4096,
			wantCPU:  4.0,
			wantPids: defaultPidsLimit,
		},
		{
			name:     "env overrides constants when caller zero",
			opts:     SandboxOptions{},
			envMem:   "8192",
			envCPU:   "8.0",
			envPids:  "1024",
			wantMem:  8192,
			wantCPU:  8.0,
			wantPids: 1024,
		},
		{
			name:     "caller wins over env",
			opts:     SandboxOptions{MemoryLimit: 1024, CPULimit: 1.0},
			envMem:   "8192",
			envCPU:   "8.0",
			envPids:  "1024",
			wantMem:  1024,
			wantCPU:  1.0,
			wantPids: 1024, // Pids has no caller field, env still wins
		},
		{
			name:     "negative caller treated as zero → env wins",
			opts:     SandboxOptions{MemoryLimit: -1, CPULimit: -2.0},
			envMem:   "4096",
			wantMem:  4096,
			wantCPU:  defaultCPUCores, // env CPU unset
			wantPids: defaultPidsLimit,
		},
		{
			name:     "garbage env → constants",
			opts:     SandboxOptions{},
			envMem:   "not-a-number",
			envCPU:   "also-garbage",
			envPids:  "",
			wantMem:  defaultMemoryMB,
			wantCPU:  defaultCPUCores,
			wantPids: defaultPidsLimit,
		},
		{
			name:     "zero env values → constants",
			opts:     SandboxOptions{},
			envMem:   "0",
			envCPU:   "0",
			envPids:  "0",
			wantMem:  defaultMemoryMB,
			wantCPU:  defaultCPUCores,
			wantPids: defaultPidsLimit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, map[string]string{
				"PUX_SANDBOX_MEMORY_MB":  tc.envMem,
				"PUX_SANDBOX_CPU_CORES":  tc.envCPU,
				"PUX_SANDBOX_PIDS":       tc.envPids,
			})
			gotMem, gotCPU, gotPids := resolveResourceDefaults(tc.opts)
			if gotMem != tc.wantMem {
				t.Errorf("memory: got %d, want %d", gotMem, tc.wantMem)
			}
			if gotCPU != tc.wantCPU {
				t.Errorf("cpu: got %f, want %f", gotCPU, tc.wantCPU)
			}
			if gotPids != tc.wantPids {
				t.Errorf("pids: got %d, want %d", gotPids, tc.wantPids)
			}
		})
	}
}

// TestEnvIntOrDefault covers the small env-or-default helpers directly.
// Garbage and zero values fall through to def — never panic, never negative.
func TestEnvIntOrDefault(t *testing.T) {
	cases := []struct {
		name  string
		env   string
		def   int
		want  int
	}{
		{"unset", "", 100, 100},
		{"empty", "", 100, 100},
		{"valid", "42", 100, 42},
		{"zero", "0", 100, 100}, // treated as invalid
		{"negative", "-5", 100, 100},
		{"garbage", "abc", 100, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, map[string]string{"TEST_INT": tc.env})
			if got := envIntOrDefault("TEST_INT", tc.def); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestEnvFloatOrDefault mirrors TestEnvIntOrDefault for floats.
func TestEnvFloatOrDefault(t *testing.T) {
	cases := []struct {
		name  string
		env   string
		def   float64
		want  float64
	}{
		{"unset", "", 1.5, 1.5},
		{"valid", "2.5", 1.5, 2.5},
		{"integer", "4", 1.5, 4.0},
		{"zero", "0", 1.5, 1.5}, // treated as invalid
		{"negative", "-2.0", 1.5, 1.5},
		{"garbage", "not-a-float", 1.5, 1.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, map[string]string{"TEST_FLOAT": tc.env})
			if got := envFloatOrDefault("TEST_FLOAT", tc.def); got != tc.want {
				t.Errorf("got %f, want %f", got, tc.want)
			}
		})
	}
}

// withEnv sets env vars for the duration of a test and restores on cleanup.
// Empty value = unset (so callers can explicitly clear a var).
func withEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	original := make(map[string]string, len(vars))
	for k, v := range vars {
		original[k] = os.Getenv(k)
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
	}
	t.Cleanup(func() {
		for k, v := range original {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	})
}
