// dispatch.go wires the org-dispatch MCP surface (dispatch_task,
// get_task_status, list_orgs) to the server-side agent loop. main.go
// constructs this runtime when PUX_LLM_API_KEY is set; if absent, the
// dispatch surface is disabled (the rest of the MCP server still works).
//
// The runtime owns:
//   - The Anthropic provider (server-side LLM client).
//   - The org loader (TOML → in-memory Org).
//   - The task store (in-memory registry of dispatched tasks).
//   - Per-org serialization mutexes (one task per org at a time).
//
// Tool dispatch inside the loop reuses the same core.Tool instances already
// registered with the MCP server — the agent loop is just another caller.

package main

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/auto-developer-orchestrator/backend/internal/agent"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/mcpserver"
	"github.com/auto-developer-orchestrator/backend/internal/org"
)

// statusSyncInterval is how often the loop's Status snapshot is mirrored
// into the task store for pollers. Trades probe latency for syscall cost.
const statusSyncInterval = 500 * time.Millisecond

// dispatchRuntime is the long-lived dispatch surface. Constructed once at
// startup; Dispatch is called per MCP request.
type dispatchRuntime struct {
	provider *agent.AnthropicProvider
	loader   *org.Loader
	store    *mcpserver.TaskStore
	logger   *zap.Logger

	// catalog is the full set of tools available to CTO/role loops. Filtered
	// per task by the org's whitelist. delegate_to is added per-task (it
	// needs an org-scoped role lookup).
	catalog []core.Tool

	// orgMuLock guards orgMu; orgMu[name] is a per-org serialization mutex.
	orgMuLock sync.Mutex
	orgMu     map[string]*sync.Mutex
}

// newDispatchRuntime wires the runtime. catalog is the full set of tools
// already registered with the MCP server — the runtime reuses these
// instances for in-loop dispatch.
func newDispatchRuntime(
	provider *agent.AnthropicProvider,
	loader *org.Loader,
	store *mcpserver.TaskStore,
	catalog []core.Tool,
	logger *zap.Logger,
) *dispatchRuntime {
	return &dispatchRuntime{
		provider: provider,
		loader:   loader,
		store:    store,
		catalog:  catalog,
		logger:   logger,
		orgMu:    make(map[string]*sync.Mutex),
	}
}

// ── Dispatcher impl ───────────────────────────────────────────────────

// Dispatch implements mcpserver.Dispatcher. Inserts the task, kicks off the
// per-org goroutine, returns the task ID immediately. Per-org serialization
// happens inside the goroutine — multiple Dispatch calls to the same org
// queue up; cross-org dispatches run concurrently.
func (r *dispatchRuntime) Dispatch(orgName, task string) (string, error) {
	o, err := r.loader.LoadByName(orgName)
	if err != nil {
		return "", fmt.Errorf("load org %q: %w", orgName, err)
	}
	if err := r.validateOrg(o); err != nil {
		return "", err
	}

	rec := r.store.Insert(orgName, task)
	ctx, cancel := context.WithCancel(context.Background())
	r.store.SetCancel(rec.ID, cancel)

	go func() {
		mu := r.orgMutex(orgName)
		mu.Lock()
		defer mu.Unlock()

		r.store.SetRunning(rec.ID)
		result, runErr := r.runOrg(ctx, o, task, rec.ID)

		if runErr != nil {
			r.logger.Warn("dispatch task failed",
				zap.String("task_id", rec.ID),
				zap.String("org", orgName),
				zap.Error(runErr))
			r.store.SetFailed(rec.ID, runErr.Error())
			return
		}
		r.store.SetComplete(rec.ID, result)
	}()

	return rec.ID, nil
}

// validateOrg sanity-checks the org's tool whitelist against the catalog.
// Failures return an error to the caller synchronously rather than failing
// the task asynchronously. Rules:
//   - Every whitelisted tool name must exist in the catalog.
//   - If the org declares roles, CTO must include delegate_to in its tools.
//   - No role may include delegate_to in its own whitelist (recursion guard).
func (r *dispatchRuntime) validateOrg(o *org.Org) error {
	available := make(map[string]struct{}, len(r.catalog)+1)
	for _, t := range r.catalog {
		available[t.Name()] = struct{}{}
	}
	available["delegate_to"] = struct{}{} // always available to CTO

	checkList := func(prefix string, names []string) error {
		for _, n := range names {
			if _, ok := available[n]; !ok {
				return fmt.Errorf("org %q: %s names unknown tool %q", o.Name, prefix, n)
			}
		}
		return nil
	}
	if err := checkList("cto.tools", o.CTO.Tools); err != nil {
		return err
	}
	if len(o.Roles) > 0 && !slices.Contains(o.CTO.Tools, "delegate_to") {
		return fmt.Errorf("org %q: cto.tools must include delegate_to when roles are declared", o.Name)
	}
	for roleName, role := range o.Roles {
		if err := agent.AssertNoDelegateInWhitelist(roleName, role.Tools); err != nil {
			return err
		}
		if err := checkList("role "+roleName+".tools", role.Tools); err != nil {
			return err
		}
	}
	return nil
}

// runOrg builds the CTO loop with org-specific tools + role lookup and runs
// it to completion. Status updates flow into the task store via a polling
// goroutine (the loop's Status struct doesn't expose a callback hook).
func (r *dispatchRuntime) runOrg(ctx context.Context, o *org.Org, task, taskID string) (string, error) {
	status := &agent.Status{}

	// Sync loop status → task store on a ticker. runnerDone ensures the
	// goroutine exits when runOrg returns even if ctx hasn't been cancelled
	// yet (ctx is cancelled by SetComplete/SetFailed in the caller).
	runnerDone := make(chan struct{})
	go func() {
		t := time.NewTicker(statusSyncInterval)
		defer t.Stop()
		for {
			select {
			case <-runnerDone:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				r.store.UpdateProgress(taskID, status.Round(), status.Tail())
			}
		}
	}()
	defer close(runnerDone)

	// Build the CTO catalog: shared sandbox tools + delegate_to (with this
	// org's role lookup).
	lookup := &orgRoleLookup{org: o, catalog: r.catalog}
	ctoCatalog := append([]core.Tool(nil), r.catalog...)
	ctoCatalog = append(ctoCatalog, agent.NewDelegateTool(lookup, r.provider, &catalogExecutor{tools: ctoCatalog}))

	ctoTools := agent.FilterTools(toolsToOpenAI(ctoCatalog), o.CTO.Tools)

	loop, err := agent.NewLoop(agent.LoopConfig{
		Provider:     r.provider,
		Executor:     &catalogExecutor{tools: ctoCatalog},
		SystemPrompt: o.CTO.Prompt,
		Tools:        ctoTools,
		MaxRounds:    o.CTO.MaxRounds,
		Status:       status,
	})
	if err != nil {
		return "", fmt.Errorf("build cto loop: %w", err)
	}

	result, runErr := loop.Run(ctx, task)

	// Final status snapshot before SetComplete fires.
	r.store.UpdateProgress(taskID, status.Round(), status.Tail())

	return result, runErr
}

// orgMutex returns the per-org serialization mutex, allocating on first use.
// Held by the dispatch goroutine for the duration of a task; the next
// Dispatch on the same org blocks here.
func (r *dispatchRuntime) orgMutex(name string) *sync.Mutex {
	r.orgMuLock.Lock()
	defer r.orgMuLock.Unlock()
	mu, ok := r.orgMu[name]
	if !ok {
		mu = &sync.Mutex{}
		r.orgMu[name] = mu
	}
	return mu
}

// ── RoleLookup impl ───────────────────────────────────────────────────

// orgRoleLookup resolves a role name to its loop config. Scoped to a single
// org (built per dispatch); lookup calls translate the role's tool whitelist
// to a filtered tool slice.
type orgRoleLookup struct {
	org     *org.Org
	catalog []core.Tool
}

func (l *orgRoleLookup) Role(name string) (agent.RoleConfig, bool) {
	role, ok := l.org.Roles[name]
	if !ok {
		return agent.RoleConfig{}, false
	}
	return agent.RoleConfig{
		Name:      role.Name,
		Prompt:    role.Prompt,
		Tools:     agent.FilterTools(toolsToOpenAI(l.catalog), role.Tools),
		MaxRounds: role.MaxRounds,
	}, true
}

// ── ToolExecutor impl ─────────────────────────────────────────────────

// catalogExecutor adapts a slice of core.Tool to core.ToolExecutor. Used by
// the loop's tool dispatcher. Lookup is by Name(); missing tools return a
// typed ToolError so the loop surfaces "[error] [toolname] ..." to the model.
type catalogExecutor struct {
	tools []core.Tool
}

func (e *catalogExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	for _, t := range e.tools {
		if t.Name() == name {
			return t.Execute(ctx, args)
		}
	}
	return nil, core.NewToolError(name, "tool not in agent catalog")
}

// toolsToOpenAI converts the catalog to the OpenAI-style tool definition
// shape the provider's tools/list API expects.
func toolsToOpenAI(tools []core.Tool) []core.OpenAITool {
	out := make([]core.OpenAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, core.OpenAITool{
			Type: "function",
			Function: core.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return out
}

// ── OrgLister impl ────────────────────────────────────────────────────

// orgLister adapts org.Loader to the OrgLister interface list_orgs expects.
type orgLister struct {
	loader *org.Loader
}

func (l *orgLister) List() ([]mcpserver.OrgSummary, error) {
	orgs, err := l.loader.LoadAll()
	if err != nil {
		return nil, err
	}
	out := make([]mcpserver.OrgSummary, 0, len(orgs))
	for _, o := range orgs {
		roles := make([]string, 0, len(o.Roles))
		for name := range o.Roles {
			roles = append(roles, name)
		}
		sort.Strings(roles)
		out = append(out, mcpserver.OrgSummary{
			Name:        o.Name,
			Description: o.Description,
			Roles:       roles,
		})
	}
	return out, nil
}
