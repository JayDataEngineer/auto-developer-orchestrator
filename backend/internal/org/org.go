// Package org loads declarative org definitions from <project>/orgs/<name>/org.toml.
//
// An org is a thin shell around:
//   - A CTO system prompt (markdown body)
//   - An optional set of role prompts (markdown bodies)
//   - A per-role tool whitelist + round budget
//
// The directory layout is:
//
//	orgs/<name>/
//	  org.toml          # config (see Org struct for fields)
//	  cto.md            # CTO system prompt body
//	  roles/<role>.md   # Role system prompt body (one per [[roles]] entry)
//
// Prompt file paths in org.toml are relative to the org directory. Roles are
// only loaded if declared in org.toml's [[roles]] array — orphan files under
// roles/ are ignored.
package org

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// Org is a loaded org definition: TOML config + the prompt bodies it
// references, all in memory. Returned by Loader.Load.
type Org struct {
	Name        string
	Description string
	Dir         string // absolute path to the org directory

	SandboxImage string   // empty = default pux-sandbox:latest
	SandboxEnv   []string // KEY=VALUE entries

	CTO   Role
	Roles map[string]Role // keyed by role.Name

	LoadedAt time.Time // for cache invalidation by directory mtime
}

// Role is a prompt + tool whitelist + round budget. The CTO is a Role too
// (Org.CTO is populated from [cto] in the TOML).
type Role struct {
	Name      string
	Prompt    string // markdown body
	MaxRounds int    // 0 = inherit server default
	Tools     []string
	Model     string // empty = inherit server default
}

// Validate checks invariants after loading. Returns a descriptive error
// naming the offending field if anything is wrong. CTO must always be set;
// Roles is optional.
func (o *Org) Validate() error {
	if o.Name == "" {
		return errors.New("org: name is required")
	}
	if o.CTO.Prompt == "" {
		return fmt.Errorf("org %q: cto prompt is required (check [cto].prompt path)", o.Name)
	}
	if o.CTO.MaxRounds < 0 {
		return fmt.Errorf("org %q: cto.max_rounds cannot be negative", o.Name)
	}
	for name, r := range o.Roles {
		if name == "" {
			return fmt.Errorf("org %q: role with empty name", o.Name)
		}
		if r.Prompt == "" {
			return fmt.Errorf("org %q: role %q prompt is empty", o.Name, name)
		}
	}
	return nil
}

// AbsPromptPath resolves a prompt path relative to the org directory.
// Used by the loader; exposed for tests.
func AbsPromptPath(orgDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(orgDir, p)
}
