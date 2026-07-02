// Package policy loads + validates declarative sandbox policies
// (orgs/<name>/policy.yaml). A policy opts a sandbox into three independent
// enforcement layers:
//
//   - workspace.mounts   — extra host bind-mounts (resolved from operator env)
//   - egress.allow       — deny-by-default network ACL (applied via iptables)
//   - credentials        — required/optional env vars validated + injected
//
// Absence of a policy file is the sentinel ErrNoPolicy — callers MUST
// treat that as "no enforcement, today's behavior" and not an error.
package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrNoPolicy is returned by Load when the org has no policy.yaml file.
// Callers MUST branch on this — it's the "feature not opted in" path.
var ErrNoPolicy = errors.New("no policy.yaml for this org")

// Policy is the in-memory representation of orgs/<name>/policy.yaml.
// All sections optional — a section is "not enforced" when nil/empty.
type Policy struct {
	Workspace   Workspace   `yaml:"workspace"`
	Egress      Egress      `yaml:"egress"`
	Credentials Credentials `yaml:"credentials"`
	Sandbox     SandboxSpec `yaml:"sandbox"`
	Browser     BrowserSpec `yaml:"browser"`
}

// SandboxSpec overrides Docker-side sandbox knobs declared by the operator.
// Empty fields = no override (caller uses its own default).
type SandboxSpec struct {
	// Image overrides the sandbox image (e.g. video-production's manim+kokoro
	// image). Empty = use caller default (typically pux-sandbox:latest).
	Image string `yaml:"image"`
	// Tier overrides the sandbox networking tier. Values: "isolated"
	// (default, bridge net + iptables egress), "bridged" (host net + host X11).
	// Empty = use caller default. Setting "isolated" on a sandbox that would
	// otherwise be bridged flips it to bridge networking + enables the
	// egress firewall.
	Tier string `yaml:"tier"`
}

// BrowserSpec configures in-sandbox browser boot-time pre-seeding. Today
// only cookie injection is supported: the operator base64-encodes a cookie
// JSON (as emitted by extract_browser_cookies.py --b64) and exports it under
// the named env var. The sandbox supervisor decodes it at boot priority 20
// and POSTs it to sb_server.py's /cookies endpoint before the agent runs.
// Cookies never touch the filesystem inside the container.
type BrowserSpec struct {
	// CookiesEnv is the name of the env var holding the base64-encoded
	// cookie JSON (e.g. "TWITTER_COOKIES_B64"). The operator MUST export
	// this var in their shell; policy_hook surfaces it to the container
	// via the existing credentials injection path. The seed-cookies.sh
	// supervisor script reads the var name from SEED_COOKIES_ENV (also
	// injected) to find the value.
	CookiesEnv string `yaml:"cookies_env"`
}

// Workspace declares extra bind-mounts + the run-as-user knob.
type Workspace struct {
	Mounts       []Mount `yaml:"mounts"`
	RunAsHostUser bool   `yaml:"run_as_host_user"`
}

// Mount is one host→container bind. Host supports ${VAR} placeholders
// resolved from operator env at sandbox-create time.
type Mount struct {
	Host      string `yaml:"host"`
	Container string `yaml:"container"`
	Mode      string `yaml:"mode"` // "rw" (default) or "ro"
}

// Egress is the deny-by-default ACL. Empty Allow = no enforcement.
type Egress struct {
	Allow []Rule `yaml:"allow"`
}

// Rule is one allowlist entry. Host may be DNS name (resolved at boot)
// or literal IP. Either Port (single) or Ports (list); never both.
type Rule struct {
	Host     string  `yaml:"host"`
	Port     int     `yaml:"port"`
	Ports    []int   `yaml:"ports"`
	Protocol string  `yaml:"protocol"` // default tcp; only tcp supported today
}

// Credentials declares required/optional env-var names. Required missing
// = refuse sandbox create. Optional = inject if present, silent skip otherwise.
type Credentials struct {
	Required []string `yaml:"required"`
	Optional []string `yaml:"optional"`
}

// ResolveTier applies the policy's sandbox.tier override to a caller-supplied
// fallback tier. Empty policy tier = no override (caller's tier wins). This
// is the single source of truth for "what tier will this sandbox actually
// boot with" — callers should consult ResolveTier rather than reading
// pol.Sandbox.Tier directly so the empty-vs-unset distinction is handled
// consistently.
//
// The caller passes the tier as a string (matching SandboxTier's underlying
// type) to avoid an import cycle. Strings are validated via the sandbox
// package's ParseSandboxTier at the apply site.
func ResolveTier(p *Policy, fallback string) string {
	if p == nil || p.Sandbox.Tier == "" {
		return fallback
	}
	return p.Sandbox.Tier
}

// Load reads orgs/<orgName>/policy.yaml under projectRoot. Returns
// ErrNoPolicy (sentinel) if the file is absent. Any other error is real
// (malformed YAML, invalid path, ...).
func Load(orgName, projectRoot string) (*Policy, error) {
	if orgName == "" {
		return nil, ErrNoPolicy
	}
	if projectRoot == "" {
		return nil, errors.New("policy.Load: projectRoot required")
	}
	path := filepath.Join(projectRoot, "orgs", orgName, "policy.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoPolicy
		}
		return nil, fmt.Errorf("policy.Load %s: %w", path, err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("policy.Load %s: %w", path, err)
	}
	// Default Protocol to "tcp" on every rule with an empty value.
	for i := range p.Egress.Allow {
		if p.Egress.Allow[i].Protocol == "" {
			p.Egress.Allow[i].Protocol = "tcp"
		}
	}
	return &p, nil
}
