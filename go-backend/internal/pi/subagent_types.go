package pi

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// SubAgentType defines the kind of specialized work a sub-agent performs.
type SubAgentType string

const (
	SubAgentCode        SubAgentType = "code"
	SubAgentExplore     SubAgentType = "explore"
	SubAgentWeb         SubAgentType = "web"
	SubAgentComputerUse SubAgentType = "computer_use"
)

// ValidSubAgentTypes is the set of allowed sub-agent types.
var ValidSubAgentTypes = map[SubAgentType]bool{
	SubAgentCode:        true,
	SubAgentExplore:     true,
	SubAgentWeb:         true,
	SubAgentComputerUse: true,
}

// SubAgentStatus represents the lifecycle state of a sub-agent.
type SubAgentStatus string

const (
	StatusPending  SubAgentStatus = "pending"
	StatusRunning  SubAgentStatus = "running"
	StatusComplete SubAgentStatus = "complete"
	StatusFailed   SubAgentStatus = "failed"
	StatusAborted  SubAgentStatus = "aborted"
)

// IsTerminal returns true if the status is a terminal state.
func (s SubAgentStatus) IsTerminal() bool {
	return s == StatusComplete || s == StatusFailed || s == StatusAborted
}

// SubAgentConfig holds the parameters for spawning a new sub-agent.
type SubAgentConfig struct {
	ProjectDir string       // filesystem path to project
	ParentID   string       // parent agent ID (e.g., "agent-12345")
	Type       SubAgentType // code, explore, web, computer_use
	Task       string       // the prompt to send to the sub-agent
	Model      string       // optional model override (e.g., "fast")
	AgentID    string       // auto-generated if empty ("sub-{type}-{ts}")
}

// InitDefaults fills in auto-generated fields if empty.
func (c *SubAgentConfig) InitDefaults() {
	if c.AgentID == "" {
		c.AgentID = fmt.Sprintf("sub-%s-%d", c.Type, time.Now().UnixMilli())
	}
}

// Validate checks the config for required fields and valid types.
func (c *SubAgentConfig) Validate() error {
	if c.ProjectDir == "" {
		return fmt.Errorf("projectDir is required")
	}
	if c.ParentID == "" {
		return fmt.Errorf("parentId is required")
	}
	if c.Task == "" {
		return fmt.Errorf("task is required")
	}
	if !ValidSubAgentTypes[c.Type] {
		validTypes := make([]string, 0, len(ValidSubAgentTypes))
		for t := range ValidSubAgentTypes {
			validTypes = append(validTypes, string(t))
		}
		return fmt.Errorf("invalid sub-agent type %q; valid types: %s", c.Type, strings.Join(validTypes, ", "))
	}
	return nil
}

// SubAgentResult holds the output of a completed sub-agent.
type SubAgentResult struct {
	SubAgentID   string        `json:"subAgentId"`
	Type         SubAgentType  `json:"type"`
	Status       SubAgentStatus `json:"status"`
	Output       string        `json:"output"`
	Error        string        `json:"error,omitempty"`
	InputTokens  float64       `json:"inputTokens"`
	OutputTokens float64       `json:"outputTokens"`
	CacheTokens  float64       `json:"cacheTokens"`
	DurationMs   int64         `json:"durationMs"`
	ToolCalls    int           `json:"toolCalls"`
}

// SubAgentInstance tracks a running sub-agent.
type SubAgentInstance struct {
	ID        string
	Config    SubAgentConfig
	Client    *PiClient
	Status    SubAgentStatus
	Result    *SubAgentResult
	Done      chan struct{} // closed when terminal state reached
	StartTime time.Time

	mu        sync.Mutex
	output    strings.Builder
	toolCount int
}
