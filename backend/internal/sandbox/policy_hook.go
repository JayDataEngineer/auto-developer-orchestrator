package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/policy"
	"github.com/moby/moby/api/types/container"
	"go.uber.org/zap"
)

// applyOrgPolicy is the integration point between sandbox.CreateSandbox
// and the policy package. It runs the full enforcement pipeline:
//
//  1. Load orgs/<OrgName>/policy.yaml (sentinel ErrNoPolicy = no-op)
//  2. Validate required credentials are present in env
//  3. Resolve workspace mount placeholders (${VAR})
//  4. Inject required + optional credentials as container env
//  5. Compute container User override ("UID:GID") when run_as_host_user is true
//  6. Stage egress allowlist to <project>/.pux/egress.conf + grant NET_ADMIN
//     capability so the in-container iptables commands actually work
//
// binds, env, and hostConfig are mutated in place. Returns the container
// User string (e.g. "1000:1000") for the caller to apply to
// container.Config.User — empty string means "no override, use image
// default". Errors abort the create.
//
// Caller is responsible for the TierBridged skip — applyOrgPolicy assumes
// the sandbox is eligible for enforcement.
func applyOrgPolicy(opts SandboxOptions, binds *[]string, env *[]string, hostConfig *container.HostConfig, log *zap.Logger) (containerUser string, err error) {
	pol, err := policy.Load(opts.OrgName, opts.ProjectPath)
	if err != nil {
		return "", fmt.Errorf("policy.load: %w", err)
	}

	// Validate required credentials present in operator env BEFORE we
	// touch Docker. Cheapest check, fails fastest, no container leak.
	if err := policy.ValidateEnv(pol); err != nil {
		return "", fmt.Errorf("policy.env: %w", err)
	}

	// Resolve workspace mounts. Host paths with unset ${VAR} fail loud.
	mounts, err := policy.ResolveMounts(pol)
	if err != nil {
		return "", fmt.Errorf("policy.mounts: %w", err)
	}
	for _, m := range mounts {
		*binds = append(*binds, fmt.Sprintf("%s:%s:%s", m.Host, m.Container, m.Mode))
		log.Info("policy: applied workspace mount",
			zap.String("container", m.Container),
			zap.String("mode", m.Mode),
		)
	}

	// Inject required + optional credentials. Required are guaranteed
	// present by ValidateEnv above; optional are best-effort.
	for _, kv := range policy.EnvVars(pol) {
		*env = append(*env, kv)
	}

	// Compute container User override. We return it for the caller to
	// apply — the User field lives on container.Config, not HostConfig,
	// and CreateSandbox builds the Config later.
	if pol.Workspace.RunAsHostUser {
		containerUser = policy.HostUser()
		log.Info("policy: container will run as host user",
			zap.String("user", containerUser),
		)
	}

	// Stage egress allowlist + grant NET_ADMIN. The script runs inside
	// the container at boot and needs the capability to mutate iptables.
	// Empty allow = no staging = no NET_ADMIN needed (preserves default
	// isolation). The conf file lives under <project>/.pux/ which is
	// bind-mounted to /sandbox/workspace/.pux/ inside the container.
	if len(pol.Egress.Allow) > 0 {
		rules, err := policy.EgressRules(pol)
		if err != nil {
			return "", fmt.Errorf("policy.egress: %w", err)
		}
		egressDir := filepath.Join(opts.ProjectPath, ".pux")
		if err := os.MkdirAll(egressDir, 0o700); err != nil {
			return "", fmt.Errorf("policy.egress: mkdir %s: %w", egressDir, err)
		}
		egressPath := filepath.Join(egressDir, "egress.conf")
		if err := os.WriteFile(egressPath, []byte(rules), 0o600); err != nil {
			return "", fmt.Errorf("policy.egress: write %s: %w", egressPath, err)
		}
		hostConfig.CapAdd = append(hostConfig.CapAdd, "NET_ADMIN")
		log.Info("policy: staged egress allowlist + granted NET_ADMIN",
			zap.String("path", egressPath),
			zap.Int("rules", len(pol.Egress.Allow)),
		)
	}

	return containerUser, nil
}


