package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/policy"
	"github.com/moby/moby/api/types/container"
	"go.uber.org/zap"
)

// PolicyDecisions bundles the policy-derived overrides that CreateSandbox
// needs to apply to its own state (image, tier) after applyOrgPolicy runs.
// Empty Image = no override. Empty Tier = no override. The container User
// override is also surfaced here for the caller to set on container.Config.
type PolicyDecisions struct {
	ContainerUser string
	Image         string
	Tier          SandboxTier // zero value (TierIsolated) is meaningful; use the bool to detect "set"
	TierSet       bool
}

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
// binds, env, and hostConfig are mutated in place. Returns PolicyDecisions
// carrying container User + image + tier overrides for the caller to apply
// (those fields live on container.Config / opts, not on hostConfig).
//
// Egress staging is skipped when the *effective* tier (after policy override)
// is TierBridged — host networking makes iptables-in-container meaningless.
// The caller passes the caller-side fallback tier so policy override can be
// resolved here in one place.
func applyOrgPolicy(opts SandboxOptions, fallbackTier SandboxTier, binds *[]string, env *[]string, hostConfig *container.HostConfig, log *zap.Logger) (PolicyDecisions, error) {
	pol, err := policy.Load(opts.OrgName, opts.ProjectPath)
	if err != nil {
		return PolicyDecisions{}, fmt.Errorf("policy.load: %w", err)
	}

	// Resolve effective tier — policy.sandbox.tier wins over caller fallback.
	effectiveTier := fallbackTier
	if pol.Sandbox.Tier != "" {
		effectiveTier = ParseSandboxTier(pol.Sandbox.Tier)
	}
	decisions := PolicyDecisions{
		Image: pol.Sandbox.Image,
		Tier:  effectiveTier,
		TierSet: pol.Sandbox.Tier != "",
	}

	// Validate required credentials present in operator env BEFORE we
	// touch Docker. Cheapest check, fails fastest, no container leak.
	if err := policy.ValidateEnv(pol); err != nil {
		return decisions, fmt.Errorf("policy.env: %w", err)
	}

	// Resolve workspace mounts. Host paths with unset ${VAR} fail loud.
	mounts, err := policy.ResolveMounts(pol)
	if err != nil {
		return decisions, fmt.Errorf("policy.mounts: %w", err)
	}
	for _, m := range mounts {
		*binds = append(*binds, fmt.Sprintf("%s:%s:%s", m.Host, m.Container, m.Mode))
		log.Info("policy: applied workspace mount",
			zap.String("container", m.Container),
			zap.String("mode", m.Mode),
		)
	}

	// Inject required + optional credentials + browser cookies env.
	// Required are guaranteed present by ValidateEnv above; optional are
	// best-effort. Cookies env (when declared + present) also injects a
	// SEED_COOKIES_ENV pointer so the supervisor script knows which var
	// to decode.
	for _, kv := range policy.EnvVars(pol) {
		*env = append(*env, kv)
	}

	// Compute container User override. Surfaced via decisions for the
	// caller to apply on container.Config.User.
	if pol.Workspace.RunAsHostUser {
		decisions.ContainerUser = policy.HostUser()
		log.Info("policy: container will run as host user",
			zap.String("user", decisions.ContainerUser),
		)
	}

	// Stage egress allowlist + grant NET_ADMIN. Skipped for effective
	// TierBridged — host networking makes iptables-in-container meaningless.
	// The script runs inside the container at boot and needs NET_ADMIN to
	// mutate iptables. Empty allow = no staging = no NET_ADMIN needed.
	// The conf file lives under <project>/.pux/ which is bind-mounted to
	// /sandbox/workspace/.pux/ inside the container.
	if effectiveTier != TierBridged && len(pol.Egress.Allow) > 0 {
		rules, err := policy.EgressRules(pol)
		if err != nil {
			return decisions, fmt.Errorf("policy.egress: %w", err)
		}
		egressDir := filepath.Join(opts.ProjectPath, ".pux")
		if err := os.MkdirAll(egressDir, 0o700); err != nil {
			return decisions, fmt.Errorf("policy.egress: mkdir %s: %w", egressDir, err)
		}
		egressPath := filepath.Join(egressDir, "egress.conf")
		if err := os.WriteFile(egressPath, []byte(rules), 0o600); err != nil {
			return decisions, fmt.Errorf("policy.egress: write %s: %w", egressPath, err)
		}
		hostConfig.CapAdd = append(hostConfig.CapAdd, "NET_ADMIN")
		log.Info("policy: staged egress allowlist + granted NET_ADMIN",
			zap.String("path", egressPath),
			zap.Int("rules", len(pol.Egress.Allow)),
		)
	}

	return decisions, nil
}


