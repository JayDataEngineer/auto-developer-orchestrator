package graph

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// TaskState represents the execution state of a task node.
type TaskState string

const (
	StatePending   TaskState = "pending"
	StateReady     TaskState = "ready"
	StateRunning   TaskState = "running"
	StateCompleted TaskState = "completed"
	StateFailed    TaskState = "failed"
	StateSkipped   TaskState = "skipped"
)

// TaskNode is a single task in the execution graph.
// Tasks declare explicit dependencies and are scheduled topologically.
type TaskNode struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Tool         string            `json:"tool"`
	ToolArgs     map[string]any    `json:"tool_args"`
	Dependencies []string          `json:"dependencies"` // IDs of tasks this depends on
	State        TaskState         `json:"state"`
	Result       any               `json:"result,omitempty"`
	Error        string            `json:"error,omitempty"`
	Priority     int               `json:"priority"`       // higher = more important (for tiebreaking)
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// TaskGraph is a directed acyclic graph (DAG) of tasks with dependency-driven scheduling.
type TaskGraph struct {
	Nodes   map[string]*TaskNode `json:"nodes"`
	RootIDs []string             `json:"root_ids"` // entry points with no dependencies

	mu sync.RWMutex
}

// NewTaskGraph creates an empty task graph.
func NewTaskGraph() *TaskGraph {
	return &TaskGraph{
		Nodes: make(map[string]*TaskNode),
	}
}

// AddNode adds a task node to the graph.
func (g *TaskGraph) AddNode(node *TaskNode) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if node.ID == "" {
		return fmt.Errorf("task node must have an ID")
	}
	if _, exists := g.Nodes[node.ID]; exists {
		return fmt.Errorf("task node %q already exists", node.ID)
	}

	node.State = StatePending
	g.Nodes[node.ID] = node
	return nil
}

// Validate checks the graph for cycles, missing dependencies, and structural issues.
func (g *TaskGraph) Validate() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Check for missing dependency references
	for id, node := range g.Nodes {
		for _, depID := range node.Dependencies {
			if _, exists := g.Nodes[depID]; !exists {
				return fmt.Errorf("task %q depends on nonexistent task %q", id, depID)
			}
		}
	}

	// Check for cycles using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for id := range g.Nodes {
		if g.hasCycle(id, visited, recStack) {
			return fmt.Errorf("cycle detected in task graph involving node %q", id)
		}
	}

	return nil
}

// hasCycle performs DFS cycle detection.
func (g *TaskGraph) hasCycle(id string, visited, recStack map[string]bool) bool {
	if recStack[id] {
		return true
	}
	if visited[id] {
		return false
	}

	visited[id] = true
	recStack[id] = true

	node := g.Nodes[id]
	for _, depID := range node.Dependencies {
		if g.hasCycle(depID, visited, recStack) {
			return true
		}
	}

	recStack[id] = false
	return false
}

// TopologicalSort returns nodes in dependency-respectful order (Kahn's algorithm).
// Returns an error if a cycle is detected.
func (g *TaskGraph) TopologicalSort() ([]string, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Calculate in-degree for each node
	inDegree := make(map[string]int)
	for id := range g.Nodes {
		inDegree[id] = 0
	}
	for _, node := range g.Nodes {
		for _, depID := range node.Dependencies {
			if _, exists := g.Nodes[depID]; exists {
				inDegree[node.ID]++
			}
		}
	}

	// Kahn's algorithm
	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		// Sort queue by priority (higher first) for deterministic ordering
		sort.Slice(queue, func(i, j int) bool {
			return g.Nodes[queue[i]].Priority > g.Nodes[queue[j]].Priority
		})

		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		// Find all nodes that depend on current
		for _, node := range g.Nodes {
			for _, depID := range node.Dependencies {
				if depID == current {
					inDegree[node.ID]--
					if inDegree[node.ID] == 0 {
						queue = append(queue, node.ID)
					}
				}
			}
		}
	}

	if len(sorted) != len(g.Nodes) {
		return nil, fmt.Errorf("cycle detected in task graph: %d nodes sorted, %d total", len(sorted), len(g.Nodes))
	}

	return sorted, nil
}

// ReadyToExecute returns task IDs that have all dependencies satisfied and are pending.
func (g *TaskGraph) ReadyToExecute() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var ready []string
	for id, node := range g.Nodes {
		if node.State != StatePending {
			continue
		}
		allDepsDone := true
		for _, depID := range node.Dependencies {
			dep, ok := g.Nodes[depID]
			if !ok {
				allDepsDone = false
				break
			}
			if dep.State != StateCompleted {
				allDepsDone = false
				break
			}
		}
		if allDepsDone {
			ready = append(ready, id)
		}
	}

	// Sort by priority (higher first)
	sort.Slice(ready, func(i, j int) bool {
		return g.Nodes[ready[i]].Priority > g.Nodes[ready[j]].Priority
	})

	return ready
}

// AllComplete returns true if all tasks are completed, failed, or skipped.
func (g *TaskGraph) AllComplete() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, node := range g.Nodes {
		if node.State == StatePending || node.State == StateReady || node.State == StateRunning {
			return false
		}
	}
	return true
}

// SetState updates a task's state.
func (g *TaskGraph) SetState(id string, state TaskState) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if node, ok := g.Nodes[id]; ok {
		node.State = state
	}
}

// SetResult sets the result of a completed task.
func (g *TaskGraph) SetResult(id string, result any, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if node, ok := g.Nodes[id]; ok {
		node.Result = result
		if err != nil {
			node.State = StateFailed
			node.Error = err.Error()
		} else {
			node.State = StateCompleted
		}
	}
}

// Stats returns statistics about the graph.
func (g *TaskGraph) Stats() map[string]int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := map[string]int{
		"total":     0,
		"pending":   0,
		"ready":     0,
		"running":   0,
		"completed": 0,
		"failed":    0,
		"skipped":   0,
	}

	for _, node := range g.Nodes {
		stats["total"]++
		switch node.State {
		case StatePending:
			stats["pending"]++
		case StateReady:
			stats["ready"]++
		case StateRunning:
			stats["running"]++
		case StateCompleted:
			stats["completed"]++
		case StateFailed:
			stats["failed"]++
		case StateSkipped:
			stats["skipped"]++
		}
	}

	return stats
}

// Visualize returns an ASCII representation of the task graph.
func (g *TaskGraph) Visualize() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("TaskGraph:\n")

	// Find roots (nodes with no dependencies)
	rootIDs := make(map[string]bool)
	for id, node := range g.Nodes {
		if len(node.Dependencies) == 0 {
			rootIDs[id] = true
		}
	}

	for id := range rootIDs {
		g.visualizeNode(&sb, id, "", true)
	}

	return sb.String()
}

func (g *TaskGraph) visualizeNode(sb *strings.Builder, id, prefix string, isLast bool) {
	node := g.Nodes[id]
	if node == nil {
		return
	}

	connector := "├──"
	if isLast {
		connector = "└──"
	}

	stateIcon := map[TaskState]string{
		StatePending:   "○",
		StateReady:     "◈",
		StateRunning:   "◉",
		StateCompleted: "✓",
		StateFailed:    "✗",
		StateSkipped:   "⊘",
	}[node.State]

	sb.WriteString(fmt.Sprintf("%s%s %s %s (%s)\n", prefix, connector, stateIcon, node.Name, node.State))

	// Find children (nodes that depend on this one)
	var children []string
	for _, n := range g.Nodes {
		for _, depID := range n.Dependencies {
			if depID == id {
				children = append(children, n.ID)
			}
		}
	}

	newPrefix := prefix
	if isLast {
		newPrefix += "    "
	} else {
		newPrefix += "│   "
	}

	for i, childID := range children {
		g.visualizeNode(sb, childID, newPrefix, i == len(children)-1)
	}
}

// FromPlan creates a task graph from a plan's step list.
// Each step gets a node with sequential dependencies by default.
func FromPlan(stepIDs, steps []string) (*TaskGraph, error) {
	if len(stepIDs) != len(steps) {
		return nil, fmt.Errorf("step IDs and steps must have the same length")
	}

	g := NewTaskGraph()
	for i, id := range stepIDs {
		var deps []string
		if i > 0 {
			deps = append(deps, stepIDs[i-1]) // sequential dependency
		}
		node := &TaskNode{
			ID:           id,
			Name:         steps[i],
			Description:  steps[i],
			Tool:         "delegate_async", // default to async delegation
			Dependencies: deps,
			State:        StatePending,
			Priority:     len(steps) - i, // earlier steps have higher priority
		}
		if err := g.AddNode(node); err != nil {
			return nil, err
		}
	}

	return g, nil
}

// FromPlanWithDeps creates a task graph with explicit dependency specs.
// Each step is a map with id, task, and optional depends_on array.
func FromPlanWithDeps(steps []map[string]any) (*TaskGraph, error) {
	g := NewTaskGraph()
	for i, step := range steps {
		id, _ := step["id"].(string)
		if id == "" {
			id = fmt.Sprintf("step_%d", i)
		}
		task, _ := step["task"].(string)
		if task == "" {
			task = fmt.Sprintf("Step %d", i+1)
		}

		var deps []string
		if depRaw, ok := step["depends_on"].([]any); ok {
			for _, d := range depRaw {
				if depID, ok := d.(string); ok {
					deps = append(deps, depID)
				}
			}
		} else if i > 0 {
			// Default: sequential dependency
			deps = append(deps, fmt.Sprintf("step_%d", i-1))
		}

		node := &TaskNode{
			ID:           id,
			Name:         task,
			Description:  task,
			Tool:         "delegate_async",
			Dependencies: deps,
			State:        StatePending,
			Priority:     len(steps) - i,
		}
		if err := g.AddNode(node); err != nil {
			return nil, err
		}
	}

	return g, nil
}
