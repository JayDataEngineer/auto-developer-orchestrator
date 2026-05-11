package sandbox

// SandboxTier controls how isolated a sub-agent's execution environment is.
//
//	"isolated" (default) — Docker container, no host access
//	"bridged"            — Docker container, host X11 + network mounted in
//	"native"             — No container, runs directly on host
type SandboxTier string

const (
	TierIsolated SandboxTier = "isolated"
	TierBridged  SandboxTier = "bridged"
	TierNative   SandboxTier = "native"
)

// ParseSandboxTier converts a string to a SandboxTier.
// Returns TierIsolated for any unrecognized value.
func ParseSandboxTier(s string) SandboxTier {
	switch s {
	case "bridged":
		return TierBridged
	case "native":
		return TierNative
	default:
		return TierIsolated
	}
}
