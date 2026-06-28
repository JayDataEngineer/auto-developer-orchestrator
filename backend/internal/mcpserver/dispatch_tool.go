package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Dispatcher is the dispatch-side hook the MCP tools call into. The actual
// implementation lives in main.go (where it has access to the org registry,
// provider, and sandbox). Defined here as an interface so the tools can be
// tested in isolation.
//
// Implementations MUST:
//   - Insert the task in the store + return its ID immediately.
//   - Start a goroutine that runs the agent loop against the org's CTO.
//   - Update the task store as the loop progresses (running → complete/failed).
type Dispatcher interface {
	Dispatch(orgName, task string) (taskID string, err error)
}

// ── dispatch_task ────────────────────────────────────────────────────

// DispatchTool is the MCP tool that kicks off a task. Returns immediately
// with a task_id; the actual work happens in a background goroutine.
type DispatchTool struct {
	store *TaskStore
	disp  Dispatcher
}

func NewDispatchTool(store *TaskStore, disp Dispatcher) *DispatchTool {
	return &DispatchTool{store: store, disp: disp}
}

func (t *DispatchTool) Name() string { return "dispatch_task" }

func (t *DispatchTool) Description() string {
	return "Dispatch a task to an org. The org's CTO receives the task " +
		"description, plans the work, and (optionally) delegates sub-tasks " +
		"to specialist employees. Returns a task_id immediately — poll " +
		"get_task_status for progress and the final result. Tasks run " +
		"asynchronously; multiple orgs can be dispatched in parallel."
}

func (t *DispatchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"org_name": {
				"type": "string",
				"description": "The org name (as returned by list_orgs)"
			},
			"task_description": {
				"type": "string",
				"description": "The task to perform. Be specific about the desired outcome — the CTO sees this verbatim."
			}
		},
		"required": ["org_name", "task_description"],
		"additionalProperties": false
	}`)
}

func (t *DispatchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	orgName, _ := args["org_name"].(string)
	taskDesc, _ := args["task_description"].(string)
	if orgName == "" {
		return nil, core.NewToolError("dispatch_task", "missing required parameter 'org_name'")
	}
	if taskDesc == "" {
		return nil, core.NewToolError("dispatch_task", "missing required parameter 'task_description'")
	}

	id, err := t.disp.Dispatch(orgName, taskDesc)
	if err != nil {
		return nil, fmt.Errorf("dispatch_task: %w", err)
	}
	return map[string]any{
		"task_id": id,
		"status":  string(StatusPending),
	}, nil
}

// ── get_task_status ──────────────────────────────────────────────────

// TaskStatusTool is the polling MCP tool. Returns the current state of a
// dispatched task plus a transcript tail for progress signal.
type TaskStatusTool struct {
	store *TaskStore
}

func NewTaskStatusTool(store *TaskStore) *TaskStatusTool {
	return &TaskStatusTool{store: store}
}

func (t *TaskStatusTool) Name() string { return "get_task_status" }

func (t *TaskStatusTool) Description() string {
	return "Poll the status of a dispatched task. Returns the current state " +
		"(pending, running, complete, failed), the result if complete, the " +
		"error if failed, the current round counter, and a transcript tail " +
		"of the last few assistant messages. Use this to wait for completion " +
		"and to read the final output."
}

func (t *TaskStatusTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_id": {
				"type": "string",
				"description": "The task_id returned by dispatch_task"
			}
		},
		"required": ["task_id"],
		"additionalProperties": false
	}`)
}

func (t *TaskStatusTool) Execute(_ context.Context, args map[string]any) (any, error) {
	id, _ := args["task_id"].(string)
	if id == "" {
		return nil, core.NewToolError("get_task_status", "missing required parameter 'task_id'")
	}
	task, ok := t.store.Get(id)
	if !ok {
		return nil, core.NewToolError("get_task_status", "unknown task_id: "+id)
	}

	resp := map[string]any{
		"task_id":    task.ID,
		"org":        task.Org,
		"status":     string(task.Status),
		"round":      task.Round,
		"started_at": task.StartedAt,
		"result":     task.Result,
		"error":      task.Error,
	}
	if len(task.Tail) > 0 {
		resp["transcript_tail"] = task.Tail
	}
	if !task.FinishedAt.IsZero() {
		resp["finished_at"] = task.FinishedAt
	}
	return resp, nil
}

// ── list_orgs ────────────────────────────────────────────────────────

// OrgLister is the minimal surface list_orgs needs. Implemented by the
// org.Loader wrapper in main.go.
type OrgLister interface {
	// List returns one entry per available org, with name + description +
	// the role names declared in its org.toml.
	List() ([]OrgSummary, error)
}

// OrgSummary is the public shape returned by list_orgs.
type OrgSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
}

// ListOrgsTool enumerates the orgs available under <project>/orgs/.
type ListOrgsTool struct {
	lister OrgLister
}

func NewListOrgsTool(lister OrgLister) *ListOrgsTool {
	return &ListOrgsTool{lister: lister}
}

func (t *ListOrgsTool) Name() string { return "list_orgs" }

func (t *ListOrgsTool) Description() string {
	return "List the orgs available in this Pux project. Each org bundles a " +
		"CTO system prompt + a set of specialist role prompts + tool " +
		"whitelists. Use this before dispatch_task to discover what " +
		"specialized agents are configured."
}

func (t *ListOrgsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`)
}

func (t *ListOrgsTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	orgs, err := t.lister.List()
	if err != nil {
		return nil, fmt.Errorf("list_orgs: %w", err)
	}
	return map[string]any{
		"orgs":  orgs,
		"count": len(orgs),
	}, nil
}
