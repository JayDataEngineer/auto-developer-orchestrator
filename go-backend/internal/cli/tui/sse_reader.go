package tui

import (
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	tea "github.com/charmbracelet/bubbletea"
)

// convertSSEEvent converts an api.SSEEvent into a tea.Msg.
func convertSSEEvent(event api.SSEEvent) interface{} {
	switch event.Type {
	case api.EventTextDelta:
		var d api.TextDeltaData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			return textDeltaMsg{text: d.Text}
		}

	case api.EventThinkingDelta:
		var d api.TextDeltaData // same shape
		if err := json.Unmarshal(event.Data, &d); err == nil {
			return thinkingDeltaMsg{text: d.Text}
		}

	case api.EventToolStart:
		var d api.ToolStartData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			args := string(d.Args)
			return toolStartMsg{name: d.ToolName, id: d.ToolID, args: args}
		}

	case api.EventToolEnd:
		var d api.ToolEndData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			return toolEndMsg{name: d.ToolName, id: d.ToolID, result: d.Result, err: d.Error}
		}

	case api.EventApprovalRequest:
		var d api.ApprovalData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			return approvalRequestMsg{
				requestID: d.RequestID,
				toolName:  d.ToolName,
				toolArgs:  d.ToolArgs,
				message:   d.Message,
				risk:      d.Risk,
			}
		}

	case api.EventArtifactCreated, api.EventArtifactUpdated:
		var d api.ArtifactData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			return artifactMsg{artifactID: d.ArtifactID, type_: d.Type, title: d.Title}
		}

	case api.EventAgentEnd:
		var d api.AgentEndData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			return doneMsg{inputTokens: d.InputTokens, outputTokens: d.OutputTokens}
		}

	case api.EventError:
		var d struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &d); err == nil {
			return errorMsg{err: d.Error}
		}
	}

	return nil
}

// readNextEvent returns a tea.Cmd that reads one event from the channel
// and converts it to a tea.Msg. This is the idiomatic Bubble Tea pattern
// for streaming: each command returns one message, and Update re-queues
// the next read.
func readNextEvent(ch <-chan api.SSEEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamEndMsg{}
		}
		msg := convertSSEEvent(event)
		if msg == nil {
			// Skip unknown events, read next one
			return readNextEvent(ch)()
		}
		return msg
	}
}
