package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"
)

var (
	agentModel      string
	agentThinking   string
	agentAutoBranch bool
	agentID         string
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Agent commands",
}

var agentPromptCmd = &cobra.Command{
	Use:   "prompt <message>",
	Short: "Send a prompt to the agent (streams SSE to stdout)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireProject(); err != nil {
			return err
		}
		client := api.NewClient(serverURL)
		message := args[0]

		effectiveProject := projectName
		effectiveOrg := ""
		// If --org is set, resolve it and pass as separate field
		if orgName != "" {
			orgPath, err := resolveOrgPath(orgName)
			if err != nil {
				return err
			}
			effectiveOrg = orgPath
			// If no project specified, use org path as project (backward compat)
			if effectiveProject == "" {
				effectiveProject = orgPath
			}
		}

		effectiveAgentID := agentID
		if effectiveAgentID == "" {
			effectiveAgentID = "default"
		}

		req := api.PromptRequest{
			Message:       message,
			Project:       effectiveProject,
			Org:           effectiveOrg,
			AgentID:       effectiveAgentID,
			Model:         agentModel,
			ThinkingLevel: agentThinking,
			AutoBranch:    agentAutoBranch,
		}

		resp, err := client.StreamPost("/api/pux/prompt", req)
		if err != nil {
			return fmt.Errorf("prompt failed: %w", err)
		}
		defer resp.Body.Close()

		if outputFmt == "json" {
			return streamJSON(resp.Body)
		}
		return streamText(resp.Body)
	},
}

var agentHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "List conversation history",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireProject(); err != nil {
			return err
		}
		client := api.NewClient(serverURL)
		var result api.HistoryResponse
		if err := client.Get("/api/pux/history?project="+projectName, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		for _, c := range result.Conversations {
			title := c.Title
			if title == "" {
				title = c.LastMessage
			}
			fmt.Printf("%s/%s  %s  (%d msgs)\n", c.Project, c.AgentID, title, c.MessageCount)
		}
		return nil
	},
}

func streamText(body io.Reader) error {
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithAutoStyle(),
	)

	ch := make(chan api.SSEEvent, 100)
	go api.StreamSSE(body, ch)

	var accumulated string
	for event := range ch {
		switch event.Type {
		case api.EventTextDelta:
			var d api.TextDeltaData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				accumulated += d.Text
				if isTTY {
					// TTY: full redraw for clean markdown rendering
					rendered, err := renderer.Render(accumulated)
					if err != nil {
						rendered = accumulated
					}
					fmt.Print("\033[H\033[2J") // clear screen for re-render
					fmt.Print(rendered)
				} else {
					// Pipe/redirect: print only the delta, no screen clear
					fmt.Print(d.Text)
				}
			}
		case api.EventThinkingDelta:
			var d api.ThinkingDeltaData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m", d.Text)
			}
		case api.EventToolStart:
			var d api.ToolStartData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[33m[TOOL] %s\033[0m\n", d.ToolName)
			}
		case api.EventToolEnd:
			var d api.ToolEndData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				if d.Error != "" {
					fmt.Fprintf(os.Stderr, "\033[31m  ✗ %s\033[0m\n", d.Error)
				} else {
					fmt.Fprintf(os.Stderr, "\033[32m  ✓\033[0m\n")
				}
			}
		case api.EventToolUpdate:
			var d api.ToolUpdateData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[2m  %s\033[0m\n", d.Text)
			}
		case api.EventSubagentStart:
			var d api.SubagentStartData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[36m→ delegating to %s: %s\033[0m\n", d.AgentName, d.Task)
			}
		case api.EventSubagentEnd:
			var d api.SubagentEndData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[32m✓ %s done (%s)\033[0m\n", d.AgentName, d.Status)
			}
		case api.EventCompactionStart:
			fmt.Fprintf(os.Stderr, "\033[2mcompressing context...\033[0m\n")
		case api.EventCompactionEnd:
			var d api.CompactionEndData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[2mcontext: %.0f%% (%s)\033[0m\n", d.ContextUtil*100, d.CompactionType)
			}
		case api.EventApprovalRequest:
			var d api.ApprovalData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[33m⚠ Approval: %s\033[0m\n", d.Title)
			}
		case api.EventUserQuestion:
			var d api.UserQuestionData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\n%s\n", d.Question)
				for _, opt := range d.Options {
					fmt.Fprintf(os.Stderr, "  - %s\n", opt)
				}
			}
		case api.EventAgentSpawned:
			// Capture silently — session continuity info
		case api.EventArtifactCreated:
			fmt.Fprintf(os.Stderr, "\033[2martifact created\033[0m\n")
		case api.EventArtifactUpdated:
			// Silent
		case api.EventPlanCreated:
			var d api.PlanCreatedData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[2mplan: %s\033[0m\n", d.Name)
			}
		case api.EventPlanUpdated:
			// Silent
		case api.EventHookRequest:
			var d api.HookRequestData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[2mhook: %s\033[0m\n", d.HookPoint)
			}
		case api.EventStepStart, api.EventStepEnd:
			// Silent — internal loop tracking
		case api.EventAgentStart:
			// Silent
		case api.EventError:
			var d struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %s\033[0m\n", d.Error)
			}
		case api.EventAgentEnd:
			var d api.AgentEndData
			if err := json.Unmarshal(event.Data, &d); err == nil {
				fmt.Fprintf(os.Stderr, "\033[2mTokens: %d in / %d out\033[0m\n", d.Input, d.Output)
			}
		}
	}

	// Final render (TTY only — pipe already got incremental output)
	if accumulated != "" && isTTY {
		rendered, _ := renderer.Render(accumulated)
		fmt.Print(rendered)
	}
	return nil
}

func streamJSON(body io.Reader) error {
	ch := make(chan api.SSEEvent, 100)
	go api.StreamSSE(body, ch)

	for event := range ch {
		fmt.Printf(`{"type":"%s","data":%s}`+"\n", event.Type, string(event.Data))
	}
	return nil
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func init() {
	agentPromptCmd.Flags().StringVar(&agentModel, "model", "", "Model override")
	agentPromptCmd.Flags().StringVar(&agentThinking, "thinking-level", "", "Thinking level")
	agentPromptCmd.Flags().BoolVar(&agentAutoBranch, "auto-branch", false, "Auto-create branch")
	agentPromptCmd.Flags().StringVar(&agentID, "agent-id", "", "Agent ID for conversation isolation (default: \"default\")")

	agentCmd.AddCommand(agentPromptCmd)
	agentCmd.AddCommand(agentHistoryCmd)
	rootCmd.AddCommand(agentCmd)
}
