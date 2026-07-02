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
