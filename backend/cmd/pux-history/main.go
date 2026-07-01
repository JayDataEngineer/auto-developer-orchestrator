// Command pux-history queries the dispatch-surface history database.
//
// Host-side operator tool. Does NOT link the mcpserver, audit, sandbox,
// or agent packages — only internal/history (+ its sqlite driver). This
// isolation is the deletion-proof contract: rm the history package +
// this binary + the 8 wiring lines in cmd/mcpserver/main.go and the MCP
// server still builds + runs identically.
//
// Usage:
//
//	PUX_HISTORY_DIR=/path/to/data pux-history list [--org NAME] [--limit N]
//	PUX_HISTORY_DIR=/path/to/data pux-history show <task-id>
//	PUX_HISTORY_DIR=/path/to/data pux-history search <regex> [--org NAME]
//
// Output is plain text (human-readable), not JSON — this is an operator
// tool, not an API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/history"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	dir := os.Getenv("PUX_HISTORY_DIR")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "PUX_HISTORY_DIR is required (set it to the same path the server writes to)")
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "list":
		runList(dir, args)
	case "show":
		runShow(dir, args)
	case "search":
		runSearch(dir, args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `pux-history — query the dispatch-surface history database

Subcommands:
  list   [--org NAME] [--limit N]   Show most-recent tasks (default limit 50)
  show   <task-id>                  Show one task's full transcript + tool calls
  search <regex> [--org NAME]       Search task bodies, messages, and tool calls

Environment:
  PUX_HISTORY_DIR   Directory containing history.sqlite (required)

Examples:
  PUX_HISTORY_DIR=/var/lib/pux pux-history list
  PUX_HISTORY_DIR=/var/lib/pux pux-history list --org _demo --limit 10
  PUX_HISTORY_DIR=/var/lib/pux pux-history show tsk_abc123
  PUX_HISTORY_DIR=/var/lib/pux pux-history search 'oauth|saml'
`)
}

// ── list ──────────────────────────────────────────────────────────────

func runList(dir string, args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	org := fs.String("org", "", "filter to one org")
	limit := fs.Int("limit", 50, "max rows to print")
	_ = fs.Parse(args)

	q, err := history.OpenQuery(dir)
	must(err)
	defer q.Close()

	tasks, err := q.ListTasks(context.Background(), *org, *limit)
	must(err)

	if len(tasks) == 0 {
		fmt.Println("(no tasks)")
		return
	}

	fmt.Printf("%-22s  %-20s  %-10s  %s\n", "TASK ID", "ORG", "STATUS", "TASK")
	fmt.Println(strings.Repeat("─", 80))
	for _, t := range tasks {
		taskPreview := truncate(t.Task, 38)
		fmt.Printf("%-22s  %-20s  %-10s  %s\n", t.ID, t.Org, t.Status, taskPreview)
	}
	fmt.Printf("\n%d task(s)\n", len(tasks))
}

// ── show ──────────────────────────────────────────────────────────────

func runShow(dir string, args []string) {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: pux-history show <task-id>")
		os.Exit(2)
	}
	taskID := fs.Arg(0)

	q, err := history.OpenQuery(dir)
	must(err)
	defer q.Close()

	ctx := context.Background()
	t, err := q.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, errNotFound) || strings.Contains(err.Error(), "no rows") {
			fmt.Fprintf(os.Stderr, "task %q not found\n", taskID)
			os.Exit(1)
		}
		must(err)
	}

	fmt.Printf("Task     %s\n", t.ID)
	fmt.Printf("Org      %s\n", t.Org)
	fmt.Printf("Status   %s\n", t.Status)
	fmt.Printf("Started  %s\n", t.StartedAt.Format(time.RFC3339))
	if !t.FinishedAt.IsZero() {
		fmt.Printf("Finished %s (%s)\n",
			t.FinishedAt.Format(time.RFC3339),
			t.FinishedAt.Sub(t.StartedAt).Round(time.Millisecond))
	}
	fmt.Printf("\nRequest:\n  %s\n\n", t.Task)

	if t.Result != "" {
		fmt.Printf("Result:\n%s\n\n", indent(t.Result, "  "))
	}
	if t.Error != "" {
		fmt.Printf("Error:\n%s\n\n", indent(t.Error, "  "))
	}

	msgs, err := q.ListMessages(ctx, taskID)
	must(err)
	calls, err := q.ListToolCalls(ctx, taskID)
	must(err)

	if len(msgs) == 0 && len(calls) == 0 {
		return
	}

	// Interleave messages + tool calls by round, then by id within round.
	// Role (cto / delegated role name) is printed alongside the round so
	// delegation chains read top-to-bottom.
	fmt.Println("Transcript:")
	for round := 1; len(msgs) > 0 || len(calls) > 0; round++ {
		any := false
		for len(msgs) > 0 && msgs[0].Round == round {
			fmt.Printf("\n[round %d | %s] assistant:\n%s\n",
				round, msgs[0].Role, indent(msgs[0].Content, "  "))
			msgs = msgs[1:]
			any = true
		}
		for len(calls) > 0 && calls[0].Round == round {
			c := calls[0]
			fmt.Printf("\n[round %d | %s] tool %s (%dms)\n",
				round, c.Role, c.Tool, c.DurationMs)
			if c.Args != "" {
				fmt.Printf("  args:   %s\n", truncate(c.Args, 400))
			}
			if c.Result != "" {
				fmt.Printf("  result: %s\n", truncate(c.Result, 400))
			}
			if c.Error != "" {
				fmt.Printf("  error:  %s\n", c.Error)
			}
			calls = calls[1:]
			any = true
		}
		if !any {
			break
		}
	}
}

// ── search ────────────────────────────────────────────────────────────

func runSearch(dir string, args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	org := fs.String("org", "", "filter to one org")
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: pux-history search <regex> [--org NAME]")
		os.Exit(2)
	}
	pattern := fs.Arg(0)
	re, err := regexp.Compile(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad regex %q: %v\n", pattern, err)
		os.Exit(2)
	}

	q, err := history.OpenQuery(dir)
	must(err)
	defer q.Close()

	hits, err := q.Search(context.Background(), re, *org, 0)
	must(err)

	if len(hits) == 0 {
		fmt.Println("(no matches)")
		return
	}

	for _, h := range hits {
		loc := h.TaskID
		if h.RowID != 0 {
			loc = fmt.Sprintf("%s #%d (round %d)", h.TaskID, h.RowID, h.Round)
		}
		fmt.Printf("[%s] %s\n  …%s…\n\n", h.Kind, loc, oneLine(h.Snippet))
	}
	fmt.Printf("%d match(es)\n", len(hits))
}

// ── helpers ───────────────────────────────────────────────────────────

// errNotFound mirrors sql.ErrNoRows; declared separately so the show
// subcommand doesn't need to import database/sql.
var errNotFound = errors.New("not found")

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// indent prefixes every line of s with prefix. Used for result + error
// bodies that may span multiple lines.
func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
