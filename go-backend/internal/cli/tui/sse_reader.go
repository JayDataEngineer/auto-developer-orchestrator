package tui

import (
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	tea "github.com/charmbracelet/bubbletea"
)

// convertSSEEvent converts an api.SSEEvent into a tea.Msg for the chat model.
func convertSSEEvent(event api.SSEEvent) tea.Msg {
	switch event.Type {
	case api.EventTextDelta:
		var d api.TextDeltaData
		if err := json.Unmarshal(event.Data, &d); err == nil && d.Text != "" {
			return textDeltaMsg{text: d.Text}
		}

	case api.EventThinkingDelta:
		var d api.TextDeltaData
		if err := json.Unmarshal(event.Data, &d); err == nil && d.Text != "" {
			return thinkingDeltaMsg{text: d.Text}
		}

	case api.EventToolStart:
		var d api.ToolStartData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			args := string(d.Args)
			// Compact JSON for display
			if len(args) > 200 {
				args = fmtCompactJSON(args, 200)
			}
			return toolStartMsg{name: d.ToolName, id: d.ToolID, args: args}
		}

	case api.EventToolEnd:
		var d api.ToolEndData
		if err := json.Unmarshal(event.Data, &d); err == nil {
			result := d.Result
			if len(result) > 400 {
				result = result[:400] + "..."
			}
			return toolEndMsg{name: d.ToolName, id: d.ToolID, result: result, err: d.Error}
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

	case api.EventSubagentStart:
		// Subagent start — not yet rendering, but don't skip
		return nil

	case api.EventSubagentEnd:
		// Subagent end — not yet rendering, but don't skip
		return nil

	case api.EventStateUpdate:
		// State updates can be skipped silently
		return nil

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

// readNextEvent returns a tea.Cmd that reads one SSE event from the channel.
// Returns the event as a tea.Msg for the Update loop.
func readNextEvent(ch <-chan api.SSEEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamEndMsg{}
		}
		msg := convertSSEEvent(event)
		if msg == nil {
			// Recursive tail-read for unknown/skipped events
			return readNextEvent(ch)()
		}
		return msg
	}
}

// fmtCompactJSON compacts a JSON string and truncates to maxLen.
func fmtCompactJSON(raw string, maxLen int) string {
	var compacted interface{}
	if err := json.Unmarshal([]byte(raw), &compacted); err != nil {
		if len(raw) <= maxLen {
			return raw
		}
		return raw[:maxLen] + "..."
	}
	out, err := json.Marshal(compacted)
	if err != nil {
		return raw
	}
	s := string(out)
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}
