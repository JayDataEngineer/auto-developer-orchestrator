package handlers

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

// SchedulerPromptSender sends prompts through the Pi agent pool on behalf of
// the scheduler. It resolves project paths, acquires Pi clients, and waits for
// agent completion.
type SchedulerPromptSender struct {
	pool        *pi.PiPool
	db          *storage.Database
	projectRoot string
}

// NewSchedulerPromptSenderAdapter creates a sender using the given pool, DB, and
// project root. The returned function is a scheduler.PromptSender.
func NewSchedulerPromptSenderAdapter(pool *pi.PiPool, db *storage.Database, projectRoot string) func(ctx context.Context, project, agentID, message, model string, autoBranch, autoMerge bool) (string, error) {
	s := &SchedulerPromptSender{
		pool:        pool,
		db:          db,
		projectRoot: projectRoot,
	}
	return s.Send
}

// Send resolves the project path, sends a prompt via Pi, and waits for
// agent_end (up to 5 minutes). Returns the accumulated assistant output.
func (s *SchedulerPromptSender) Send(ctx context.Context, project, agentID, message, model string, autoBranch, autoMerge bool) (string, error) {
	// Resolve project path from database or default projects dir
	projectPath := resolveProjectPath(project, s.db)
	if projectPath == "" {
		// Fallback: construct path from projectRoot
		projectPath = s.projectRoot + "/" + project
		if _, err := os.Stat(projectPath); err != nil {
			return "", fmt.Errorf("project %s not found", project)
		}
	}

	client, err := s.pool.GetOrCreateWithID(projectPath, agentID)
	if err != nil {
		return "", fmt.Errorf("failed to get Pi client: %w", err)
	}
	if err := client.SendPrompt(message, model, ""); err != nil {
		return "", fmt.Errorf("failed to send prompt: %w", err)
	}

	// Wait for agent_end event (with timeout)
	subID := fmt.Sprintf("sched-%d", time.Now().UnixNano())
	events := client.Subscribe(subID)
	defer client.Unsubscribe(subID)

	var output string
	timeout := time.After(5 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		case <-timeout:
			return output, fmt.Errorf("scheduled job timed out")
		case event, ok := <-events:
			if !ok {
				return output, nil
			}
			if event.Type == pi.RpcEventAgentEnd {
				return output, nil
			}
			if event.AssistantMessageEvent != nil && event.AssistantMessageEvent.Type == "text_delta" {
				output += event.AssistantMessageEvent.Delta
			}
		}
	}
}
