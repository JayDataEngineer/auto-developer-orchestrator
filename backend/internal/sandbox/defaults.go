package sandbox

import (
	"os"
	"strconv"
)

// Resource limit defaults. Applied when the caller passes zero values for
// MemoryLimit / CPULimit. PidsLimit has no caller override — it's always
// set from PUX_SANDBOX_PIDS (default 512) because there's no legitimate
// reason for an agent sandbox to need more processes than that.
//
// 2 GB / 2 cores / 512 pids covers normal agent workload (bash + python +
// browser + ONNX vision in one sandbox) while leaving headroom on the host
// for the operator's other work. Override per-invocation via env when an
// org genuinely needs more (video-production rendering, large model
// inference, etc.) — policy.yaml overrides are a future addition.
const (
	defaultMemoryMB  = 2048
	defaultCPUCores  = 2.0
	defaultPidsLimit = 512
)

// resolveResourceDefaults applies env-overridable defaults to the
// caller-supplied resource fields. Caller-supplied non-zero values win.
// PidsLimit always comes from env (or its default) — see comment above.
//
// Extracted as a pure function so the resolution rules have a unit test
// that doesn't need a Docker client. CreateSandbox is the only caller.
func resolveResourceDefaults(opts SandboxOptions) (memoryMB int, cpuCores float64, pids int) {
	memoryMB = opts.MemoryLimit
	if memoryMB <= 0 {
		memoryMB = envIntOrDefault("PUX_SANDBOX_MEMORY_MB", defaultMemoryMB)
	}

	cpuCores = opts.CPULimit
	if cpuCores <= 0 {
		cpuCores = envFloatOrDefault("PUX_SANDBOX_CPU_CORES", defaultCPUCores)
	}

	pids = envIntOrDefault("PUX_SANDBOX_PIDS", defaultPidsLimit)
	return memoryMB, cpuCores, pids
}

// envIntOrDefault parses $key as an int, returning def on missing/invalid.
// Same shape as getEnvOrDefault but for ints — kept local to this file
// because the only callers are resource-limit resolution.
func envIntOrDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// envFloatOrDefault parses $key as a float, returning def on missing/invalid.
func envFloatOrDefault(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return def
	}
	return f
}
