// Command pux-tui is a Bubble Tea conversational interface for the Pux
// MCP server. It points at a running pux-mcpserver over HTTP (same wire
// contract as Claude Desktop / any other MCP client) and lets the operator
// chat with an org's CTO.
//
// CONVERSATION STATE LIVES CLIENT-SIDE. The dispatch surface is intentionally
// stateless per-task — each Enter dispatches the full accumulated conversation
// as a single task_description, which the CTO sees verbatim. This is the only
// way to get multi-turn chat against the slim MVP server.
//
// ISOLATION CONTRACT: this binary does NOT link agent/, mcpserver/, sandbox/,
// org/, audit/, sensitive/, adapters/, tools/, or core/. It's a pure HTTP
// client + the internal/tui package. rm -rf this cmd + internal/tui/ and the
// MCP server still builds + runs identically.
//
// Usage:
//
//	pux-tui --mcp-addr http://127.0.0.1:9987 --org _demo
//	pux-tui                          # http://127.0.0.1:9987, no preselected org
//
// The history pane auto-enables if PUX_HISTORY_DIR points at an existing
// history.sqlite (probed via os.Stat; OpenQuery would create the file).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/auto-developer-orchestrator/backend/internal/tui"
)

func main() {
	mcpAddr := flag.String("mcp-addr", "http://127.0.0.1:9987",
		"Address of the running pux-mcpserver")
	org := flag.String("org", "",
		"Org name to talk to (optional — picked at runtime if omitted)")
	flag.Parse()

	if !strings.HasPrefix(*mcpAddr, "http://") && !strings.HasPrefix(*mcpAddr, "https://") {
		*mcpAddr = "http://" + *mcpAddr
	}

	// Health-check the MCP server before launching the TUI — failing here is
	// much friendlier than an opaque "Init" error after the program takes
	// over the terminal.
	if err := pingMCPServer(*mcpAddr); err != nil {
		fmt.Fprintf(os.Stderr, "pux-tui: can't reach MCP server at %s: %v\n", *mcpAddr, err)
		fmt.Fprintln(os.Stderr, "(start it with: task run)")
		os.Exit(1)
	}

	client := tui.NewMCPClient(*mcpAddr)
	hist := tui.MaybeLoadHistoryPane()
	defer hist.Close()

	if err := client.Init(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "pux-tui: MCP handshake failed: %v\n", err)
		os.Exit(1)
	}

	// If no --org was passed but list_orgs returns exactly one org, preselect
	// it. Otherwise leave the field blank — the model renders a hint and the
	// user has to restart with --org.
	resolvedOrg := *org
	if resolvedOrg == "" {
		if orgs, err := client.ListOrgs(context.Background()); err == nil && len(orgs) == 1 {
			resolvedOrg = orgs[0].Name
		} else if err == nil && len(orgs) == 0 {
			fmt.Fprintln(os.Stderr, "pux-tui: no orgs found under <project>/orgs/ — create one before launching")
			os.Exit(1)
		}
	}

	m := tui.NewModel(client, hist, resolvedOrg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "pux-tui: program error: %v\n", err)
		os.Exit(1)
	}
}

// pingMCPServer does a quick connectivity check. We don't run initialize
// here — that's MCPClient.Init's job, called after this succeeds. The
// health check just confirms the server is up + answering HTTP.
func pingMCPServer(addr string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(addr, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":"ping","method":"ping","params":{}}`))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Any 2xx response means the server is alive. 4xx/5xx means it's reachable
	// but rejecting us — also "alive" enough to proceed.
	return nil
}
