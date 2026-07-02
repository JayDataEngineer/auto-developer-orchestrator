package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// placeholderRe matches ${VAR_NAME} in policy strings.
var placeholderRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ErrMissingCreds is returned by ValidateEnv when one or more required
// credentials are absent from the operator environment. The error string
// lists every missing name so the operator sees the full repair list in
// one shot, not one round-trip per credential.
type ErrMissingCreds struct {
	Missing []string
}

func (e *ErrMissingCreds) Error() string {
	return "missing required credentials: " + strings.Join(e.Missing, ", ")
}

// ValidateEnv checks that every required credential is present in the
// operator environment. Optional credentials are not checked here —
// absence is silent (they're injected if present, skipped if not).
//
// Returns *ErrMissingCreds (typed) when one or more are missing, so
// callers can branch on the kind of failure cleanly.
func ValidateEnv(p *Policy) error {
	if p == nil {
		return nil
	}
	var missing []string
	for _, name := range p.Credentials.Required {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return &ErrMissingCreds{Missing: missing}
	}
	return nil
}

// EnvVars returns the KEY=VALUE strings to inject into the container env.
// Required creds come straight from os.Getenv (ValidateEnv already proved
// they're set). Optional creds are included only when present in env.
// When browser.cookies_env is declared and that env var is set, two entries
// are emitted: the cookies value under the operator-named var (so the
// seed-cookies supervisor script can find it) plus SEED_COOKIES_ENV=<name>
// (so the script knows which var to read).
// Caller passes the result to Docker's Env field.
func EnvVars(p *Policy) []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, name := range p.Credentials.Required {
		out = append(out, name+"="+os.Getenv(name))
	}
	for _, name := range p.Credentials.Optional {
		if v := os.Getenv(name); v != "" {
			out = append(out, name+"="+v)
		}
	}
	if p.Browser.CookiesEnv != "" {
		if v := os.Getenv(p.Browser.CookiesEnv); v != "" {
			out = append(out, p.Browser.CookiesEnv+"="+v)
			out = append(out, "SEED_COOKIES_ENV="+p.Browser.CookiesEnv)
		}
	}
	return out
}

// ResolvedMount is one host→container bind with ${VAR} expanded.
type ResolvedMount struct {
	Host      string
	Container string
	Mode      string // "rw" or "ro"
}

// ErrUnresolvedMount is returned by ResolveMounts when a ${VAR} placeholder
// in a mount's Host field has no matching env var. Failing loud beats
// silently mounting the wrong directory or skipping the mount entirely.
type ErrUnresolvedMount struct {
	Container   string
	Unresolved  string // the literal ${VAR} string
	MissingVar  string // the bare VAR name
}

func (e *ErrUnresolvedMount) Error() string {
	return fmt.Sprintf(
		"mount %s: host %q references unset env var %s",
		e.Container, e.Unresolved, e.MissingVar,
	)
}

// ResolveMounts walks p.Workspace.Mounts, expands ${VAR} placeholders in
// Host fields against the operator env, validates container paths, and
// normalizes Mode. Returns ErrUnresolvedMount (typed) on the first
// missing var — fail-fast, don't silently degrade.
//
// Container paths must be absolute (Docker requirement); relative paths
// are rejected with a plain error.
func ResolveMounts(p *Policy) ([]ResolvedMount, error) {
	if p == nil || len(p.Workspace.Mounts) == 0 {
		return nil, nil
	}
	out := make([]ResolvedMount, 0, len(p.Workspace.Mounts))
	for _, m := range p.Workspace.Mounts {
		host, err := expandPlaceholders(m.Host, m.Container)
		if err != nil {
			return nil, err
		}
		if !filepath.IsAbs(m.Container) {
			return nil, fmt.Errorf(
				"mount %s: container path must be absolute, got %q",
				m.Container, m.Container,
			)
		}
		mode := m.Mode
		if mode == "" {
			mode = "rw"
		}
		if mode != "rw" && mode != "ro" {
			return nil, fmt.Errorf(
				"mount %s: mode must be 'rw' or 'ro', got %q",
				m.Container, mode,
			)
		}
		out = append(out, ResolvedMount{
			Host:      host,
			Container: m.Container,
			Mode:      mode,
		})
	}
	return out, nil
}

// expandPlaceholders replaces ${VAR} with os.Getenv("VAR"). Returns
// ErrUnresolvedMount (typed) on the first unset var so callers can give
// a precise error pointing at which mount's host is broken.
func expandPlaceholders(value, containerPath string) (string, error) {
	var firstUnresolved *ErrUnresolvedMount
	expanded := placeholderRe.ReplaceAllStringFunc(value, func(match string) string {
		// match is "${VAR}"; strip ${ and } to get the bare name.
		varName := match[2 : len(match)-1]
		if v := os.Getenv(varName); v != "" {
			return v
		}
		if firstUnresolved == nil {
			firstUnresolved = &ErrUnresolvedMount{
				Container:  containerPath,
				Unresolved: match,
				MissingVar: varName,
			}
		}
		return match
	})
	if firstUnresolved != nil {
		return "", firstUnresolved
	}
	return expanded, nil
}

// ErrEmptyHost is returned when a mount's Host field is empty after
// placeholder expansion (or was empty to begin with).
var ErrEmptyHost = errors.New("mount host is empty after expansion")

// HostUser returns the "UID:GID" string for the host user, suitable for
// Docker's hostConfig.User field. Only meaningful when
// p.Workspace.RunAsHostUser is true.
func HostUser() string {
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}
