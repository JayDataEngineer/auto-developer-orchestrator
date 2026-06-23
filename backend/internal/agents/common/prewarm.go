package common

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/auto-developer-orchestrator/backend/internal/extensions"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
)

// PreWarmer walks every capability's implementations[] at boot. For any tier
// that declares `source: git+...`, it clones (if allowlisted), runs the
// bringup command, and registers the result as an MCP client under the prefix
// `<capability>-<tier>`. Failed pre-warms log + move on — they don't block boot.
//
// Registered clients are OUT of the routing map (which still points at the
// active tier) until Layer 3's hot-promotion path hands them off. Layer 2 just
// ensures the clone is warm so promotion is a port-swap, not a 2-minute wait.
type PreWarmer struct {
	mgr     *extensions.Manager
	mc      *mcp.MultiClient
	logger  *zap.Logger

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewPreWarmer wires the PreWarmer to the extension manager (for CloneAndStart)
// and the MCP MultiClient (to register the pre-warmed clients).
func NewPreWarmer(mgr *extensions.Manager, mc *mcp.MultiClient, logger *zap.Logger) *PreWarmer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PreWarmer{mgr: mgr, mc: mc, logger: logger}
}

// Start spawns a goroutine that walks LoadToolPackages() and pre-warms every
// self-hostable tier. Returns immediately. Call Stop() at shutdown to avoid
// goroutine leaks.
//
// Pre-warm is best-effort: a missing allowlist, a failed clone, or a bringup
// timeout all log + skip. The active tier chosen at ResolveAll is unaffected
// by pre-warm results — pre-warm only adds fallbacks.
func (p *PreWarmer) Start(parent context.Context) {
	if p.mgr == nil || p.mc == nil {
		p.logger.Warn("pre-warmer disabled: nil manager or multiclient")
		return
	}

	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel

	p.wg.Add(1)
	go p.run(ctx)
}

// Stop signals the pre-warm goroutine to exit and blocks until it does.
// Safe to call multiple times; second call is a no-op.
func (p *PreWarmer) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

// run is the goroutine body. Iterates LoadToolPackages, filters to self-hostable
// tiers, calls CloneAndStart serially (sequential is intentional — pre-warm is
// a cold-boot cost, parallel clone+install would thrash disk + network).
func (p *PreWarmer) run(ctx context.Context) {
	defer p.wg.Done()

	pkgs := LoadToolPackages()
	for capName, pkg := range pkgs {
		if ctx.Err() != nil {
			return
		}
		if len(pkg.Implementations) == 0 {
			continue
		}
		for _, impl := range pkg.Implementations {
			if ctx.Err() != nil {
				return
			}
			if impl.Source == "" {
				continue
			}
			p.preWarmOne(ctx, capName, &impl)
		}
	}
}

// preWarmOne handles a single self-hostable tier: trust check, clone, register.
// Errors are logged and swallowed — pre-warm is best-effort.
func (p *PreWarmer) preWarmOne(ctx context.Context, capName string, impl *Implementation) {
	if !IsTrusted(impl.Source) {
		p.logger.Warn("pre-warm skipped: source not in trusted-repos.txt",
			zap.String("capability", capName),
			zap.String("tier", impl.Name),
			zap.String("source", impl.Source),
			zap.String("hint", "add the URL to ~/.pux/trusted-repos.txt to enable"))
		return
	}

	prefix := fmt.Sprintf("%s-%s", capName, impl.Name)
	p.logger.Info("pre-warming tier",
		zap.String("capability", capName),
		zap.String("tier", impl.Name),
		zap.String("source", impl.Source),
		zap.String("prefix", prefix))

	// CloneAndStart spawns the long-running subprocess. Cap the wait at the
	// bringup_timeout (default 120s) so a hung clone doesn't block the rest of
	// the walk.
	timeout := time.Duration(impl.Health.BringupTimeout) * time.Second
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	warmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	port, err := p.mgr.CloneAndStart(warmCtx, impl.Source, impl.Bringup, prefix)
	if err != nil {
		p.logger.Warn("pre-warm failed",
			zap.String("capability", capName),
			zap.String("tier", impl.Name),
			zap.String("source", impl.Source),
			zap.Error(err))
		return
	}

	// Register the pre-warmed subprocess as an MCP client. It's NOT the active
	// tier (unless the resolver already picked it at boot); it's a warm standby
	// that Layer 3's hot-promotion path can swap in.
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	client := mcp.NewClient(prefix, endpoint, p.logger)
	p.mc.AddClient(prefix, client)

	p.logger.Info("pre-warm complete",
		zap.String("capability", capName),
		zap.String("tier", impl.Name),
		zap.String("prefix", prefix),
		zap.Int("port", port))
}
