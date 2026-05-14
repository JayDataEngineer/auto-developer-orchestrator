package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/autoconfig"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// PendingPlans is a global registry of plans waiting for user approval.
// The create_plan tool registers here, and the HTTP handler for /api/pux/plan-response resolves.
var PendingPlans = &planRegistry{
	entries: make(map[string]chan PlanResponse),
}

// PlanResponse is the user's response to a plan approval request.
type PlanResponse struct {
	Action   string // "approve", "refine", "cancel"
	Feedback string // Optional user feedback (for refine)
}

type planRegistry struct {
	mu      sync.Mutex
	entries map[string]chan PlanResponse // key = planID, value = response channel
}

func (r *planRegistry) Register(id string) chan PlanResponse {
	ch := make(chan PlanResponse, 1)
	r.mu.Lock()
	r.entries[id] = ch
	r.mu.Unlock()
	return ch
}

func (r *planRegistry) Resolve(id string, response PlanResponse) bool {
	r.mu.Lock()
	ch, ok := r.entries[id]
	if ok {
		delete(r.entries, id)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- response
	return true
}

// PlanTool lets the AI create an execution plan that must be approved by the user.
// The plan is persisted to .pux/plans/{name}.md and blocks until the user responds.
// File I/O goes through PlanStore (ArtifactStore contract) for path safety and consistency.
//
// Contract 3 compliance: does NOT take an SSE subscriber in the constructor.
// Retrieves it from context (set by AgentLoop) when needed.
type PlanTool struct {
	projectDir string
	store      *autoconfig.PlanStore
}

func NewPlanTool(projectDir string, store *autoconfig.PlanStore) *PlanTool {
	return &PlanTool{projectDir: projectDir, store: store}
}

func (t *PlanTool) Name() string { return "create_plan" }

func (t *PlanTool) Description() string {
	return "Create an execution plan for complex tasks. The plan is saved to a file and must be approved by the user before execution begins. Use this for tasks that require multiple steps, architectural decisions, or changes across 3+ files. The plan persists across sessions and is automatically referenced on subsequent turns."
}

func (t *PlanTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Short kebab-case name for the plan (e.g., 'refactor-auth', 'add-user-settings')"},
			"content": {"type": "string", "description": "The plan content in markdown format. Include: context, approach, files to modify, verification steps."}
		},
		"required": ["name", "content"]
	}`)
}

func (t *PlanTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("create_plan: name is required")
	}
	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("create_plan: content is required")
	}

	// Sanitize name: only allow alphanumeric, dashes, underscores
	safeName := sanitizePlanName(name)
	if safeName == "" {
		return nil, fmt.Errorf("create_plan: invalid name (use lowercase letters, numbers, dashes)")
	}

	// Write plan through PlanStore (ArtifactStore contract — path safety, name validation)
	header := fmt.Sprintf("# Plan: %s\n\nCreated: %s\n\n", name, time.Now().Format("2006-01-02 15:04:05"))
	spec := map[string]any{"content": header + content}
	result, err := t.store.Put(context.Background(), safeName, spec)
	if err != nil {
		return nil, fmt.Errorf("create_plan: %w", err)
	}

	// Derive plan path for event emission
	plansDir := filepath.Join(t.projectDir, ".pux", "plans")
	planPath := filepath.Join(plansDir, safeName+".md")

	// Generate unique plan ID
	planID := fmt.Sprintf("p_%d_%s", time.Now().UnixNano(), safeName)
	_ = result // result message used implicitly

	// Register pending plan
	responseCh := PendingPlans.Register(planID)

	// Contract 3: emit plan_created event via context-provided subscriber.
	// The agent loop injects the subscriber into context (SubscriberKey).
	// Tools do NOT hold direct references to the event stream.
	if sub, ok := ctx.Value(core.SubscriberKey{}).(chan core.AgentEvent); ok && sub != nil {
		core.SendEvent(sub, core.AgentEvent{
			Type: core.EventTypePlanCreated,
			Data: core.AgentEventData{
				ToolID:   planID,
				ToolName: "create_plan",
				ToolArgs: map[string]any{
					"planId":   planID,
					"name":     name,
					"content":  content,
					"filePath": planPath,
				},
			},
		})
	}

	// Block until response arrives or context cancels (10 minute timeout)
	select {
	case response := <-responseCh:
		switch response.Action {
		case "approve":
			return map[string]any{
				"approved": true,
				"message":  "Plan approved by user. Proceed with execution.",
				"filePath": planPath,
			}, nil
		case "refine":
			return map[string]any{
				"approved": false,
				"refine":   true,
				"feedback": response.Feedback,
				"message":  fmt.Sprintf("User wants plan refined: %s", response.Feedback),
				"filePath": planPath,
			}, nil
		case "cancel":
			return map[string]any{
				"approved": false,
				"cancelled": true,
				"message":  "Plan cancelled by user.",
			}, nil
		default:
			return map[string]any{
				"approved": false,
				"message":  fmt.Sprintf("Unknown action: %s", response.Action),
			}, nil
		}
	case <-ctx.Done():
		PendingPlans.Resolve(planID, PlanResponse{Action: "cancel"}) // cleanup
		return nil, fmt.Errorf("create_plan: cancelled (user did not respond)")
	case <-time.After(10 * time.Minute):
		PendingPlans.Resolve(planID, PlanResponse{Action: "cancel"}) // cleanup
		return nil, fmt.Errorf("create_plan: timed out after 10 minutes waiting for user approval")
	}
}

// sanitizePlanName removes unsafe characters from plan names.
func sanitizePlanName(name string) string {
	var b strings.Builder
	prev := byte('_')
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c)
			prev = c
		case c >= '0' && c <= '9':
			b.WriteByte(c)
			prev = c
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c + 32) // to lowercase
			prev = c + 32
		case c == '-' || c == '_' || c == ' ':
			if prev != '-' { // avoid consecutive dashes
				b.WriteByte('-')
				prev = '-'
			}
		}
	}
	result := strings.Trim(b.String(), "-")
	// Limit length
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

// InjectActivePlan reads the most recent plan file and returns it formatted for
// injection into the agent's context prefix. Returns empty string if no plans exist.
func InjectActivePlan(projectDir string) string {
	plansDir := filepath.Join(projectDir, ".pux", "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil || len(entries) == 0 {
		return ""
	}

	// Filter .md files and sort by modification time (most recent first)
	type planFile struct {
		name    string
		modTime time.Time
	}
	var files []planFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, planFile{name: e.Name(), modTime: info.ModTime()})
	}
	if len(files) == 0 {
		return ""
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Read the most recent plan
	data, err := os.ReadFile(filepath.Join(plansDir, files[0].name))
	if err != nil {
		return ""
	}

	return "<active_plan>\n" + string(data) + "\n</active_plan>\n\n"
}
