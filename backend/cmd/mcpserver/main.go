// Command mcpserver runs the Pux MCP server.
//
// Single-tenant, localhost-only. Boots a Docker sandbox, registers the
// sandbox-aware tools (bash, file_read, file_write, file_edit, file_grep,
// file_glob, python) as MCP tools, and serves JSON-RPC over HTTP on
// 127.0.0.1.
//
// Subcommands:
//
//	mcpserver run      [flags]   foreground (default if no subcommand)
//	mcpserver start    [flags]   daemonize, return immediately
//	mcpserver stop     [flags]   SIGTERM the daemonized server
//	mcpserver status   [flags]   report running state
//
// Backward compatibility: if the first arg is missing or starts with `-`
// (a flag), the binary defaults to `run`. This keeps `mcpserver --addr X
// --project Y` and existing scripts working unchanged.
//
//	PUX_PROJECT_PATH=/path/to/workspace mcpserver
//
// Connect from Claude Desktop / Hermes / OpenClaw by adding to the client's
// MCP config:
//
//	{
//	  "mcpServers": {
//	    "pux": {
//	      "url": "http://127.0.0.1:9987"
//	    }
//	  }
//	}
//
// Auth: localhost-only, no auth. For tailnet exposure, run a reverse proxy
// (Caddy/Tailscale Funnel) in front.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/agent"
	"github.com/auto-developer-orchestrator/backend/internal/audit"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/history"
	"github.com/auto-developer-orchestrator/backend/internal/mcpserver"
	"github.com/auto-developer-orchestrator/backend/internal/org"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
)

const version = "0.1.0-mvp"

func main() {
	cmd := "run"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	switch cmd {
	case "run":
		runRun(args)
	case "start":
		runStart(args)
	case "stop":
		runStop(args)
	case "status":
		runStatus(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mcpserver — Pux MCP server with lifecycle subcommands

Subcommands:
  run      [flags]   Boot in foreground (default if no subcommand given)
  start    [flags]   Daemonize + return immediately (writes PID file)
  stop     [flags]   SIGTERM the daemonized server (PID file lookup)
  status   [flags]   Report running state from the PID file

Common flags (run + start):
  --addr         Listen address (default 127.0.0.1:9987, $PUX_MCP_ADDR)
  --project      Project directory mounted into the sandbox ($PUX_PROJECT_PATH)
  --sandbox-id   Sandbox ID (default mcp-default, $PUX_SANDBOX_ID)
  --keep-alive   Leave sandbox running on exit (default: destroy)

Stop flags:
  --project      Where to look for the PID file (default $PWD)
  --wait         Seconds to wait for SIGTERM before SIGKILL (default 10)

Status flags:
  --project      Where to look for the PID file (default $PWD)
  --live         POST a ping to verify the server actually responds

Environment:
  PUX_PID_FILE         Override the PID file location
  PUX_MCP_ADDR         Override --addr
  PUX_PROJECT_PATH     Override --project
  PUX_SANDBOX_ID       Override --sandbox-id
  PUX_AUDIT_LOG        Opt-in audit log path (JSONL)
  PUX_LLM_API_KEY      Opt-in dispatch surface (org → agent loop)
  PUX_HISTORY_DIR      Opt-in history recorder (sqlite)

Examples:
  mcpserver                                   # foreground, default port
  mcpserver start --project ~/code/myproj     # daemonize
  mcpserver status --project ~/code/myproj    # is it up?
  mcpserver stop --project ~/code/myproj      # tear it down
`)
}

// runRun is the foreground boot path. Writes a PID file at sandbox-ready,
// removes it on clean signal-driven shutdown. Crashes leave a stale PID
// file for the next `start` to detect.
func runRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	addr := fs.String("addr", envOrDefault("PUX_MCP_ADDR", "127.0.0.1:9987"), "Listen address (localhost-only by default)")
	projectPath := fs.String("project", envOrDefault("PUX_PROJECT_PATH", ""), "Project directory mounted into the sandbox (defaults to CWD)")
	sandboxID := fs.String("sandbox-id", envOrDefault("PUX_SANDBOX_ID", "mcp-default"), "Sandbox ID")
	keepAlive := fs.Bool("keep-alive", false, "Leave sandbox running on exit (default: destroy)")
	_ = fs.Parse(args)

	*projectPath = resolveProjectPath(*projectPath)

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("pux mcp server starting",
		zap.String("version", version),
		zap.String("addr", *addr),
		zap.String("project", *projectPath),
		zap.String("sandbox_id", *sandboxID),
	)

	pidPath := resolvePIDFile(*projectPath)

	// 1. Sandbox manager.
	mgr, err := sandbox.NewManager(logger)
	if err != nil {
		logger.Fatal("sandbox manager init failed — is Docker running?", zap.Error(err))
	}

	// 2. Create the singleton sandbox.
	sb, err := mgr.CreateSandbox(context.Background(), sandbox.SandboxOptions{
		ID:          *sandboxID,
		ProjectPath: *projectPath,
	})
	if err != nil {
		var ve *sandbox.ValidationError
		if errors.As(err, &ve) {
			logger.Fatal("sandbox validation failed", zap.Error(err))
		}
		logger.Fatal("create sandbox failed", zap.Error(err))
	}
	logger.Info("sandbox ready",
		zap.String("sandbox_id", sb.ID),
		zap.String("container_id", sb.ContainerID),
	)

	// 3. Write the PID file now that we have sandbox metadata. This is the
	//    readiness signal `start` polls for.
	entry := pidFileEntry{
		PID:         os.Getpid(),
		Addr:        *addr,
		Project:     *projectPath,
		SandboxID:   sb.ID,
		ContainerID: sb.ContainerID,
		StartedAt:   time.Now().UTC(),
	}
	// Best-effort: stale cleanup if a previous run crashed.
	if _, err := os.Stat(pidPath); err == nil {
		// File exists — assume stale (we'd have refused in `start`).
		_ = removePIDFile(pidPath)
	}
	if err := writePIDFile(pidPath, entry); err != nil {
		logger.Warn("pid file write failed (non-fatal — supervisor stop/status will not work)", zap.Error(err))
	}
	logger.Info("pid file written", zap.String("path", pidPath))

	// 4. Adapters bridge the sandbox manager to the tool implementations.
	bashExec := &adapters.BashExecutor{Mgr: mgr, SandboxID: sb.ID}
	fileOps := &adapters.FileOps{Mgr: mgr, SandboxID: sb.ID}

	// 5. Build the tool registry.
	auditLogger, err := audit.Open(os.Getenv("PUX_AUDIT_LOG"))
	if err != nil {
		logger.Fatal("audit log open failed", zap.Error(err))
	}
	defer auditLogger.Close()
	if auditLogger != nil {
		logger.Info("audit log enabled",
			zap.String("path", os.Getenv("PUX_AUDIT_LOG")))
	}

	srv := mcpserver.New("pux-mcp", version, auditLogger)
	srv.RegisterTool(bash.New(bashExec))
	srv.RegisterTool(file.NewReadTool(fileOps))
	srv.RegisterTool(file.NewWriteTool(fileOps))
	srv.RegisterTool(file.NewEditTool(fileOps))
	srv.RegisterTool(file.NewGrepTool(fileOps))
	srv.RegisterTool(file.NewGlobTool(fileOps))
	srv.RegisterTool(mcpserver.NewSandboxPythonTool(bashExec))
	srv.RegisterTool(mcpserver.NewListSkillsTool(*projectPath))
	srv.RegisterTool(mcpserver.NewLoadSkillTool(*projectPath))
	// Vision is opt-in: the tool returns a friendly "run bootstrap-vision.sh"
	// message when the model isn't downloaded, so it's always safe to register.
	srv.RegisterTool(mcpserver.NewDescribeImageTool(bashExec, mcpserver.VisionToolConfig{}))
	// Browser tools wrap the sandbox's sb_server.py (SeleniumBase HTTP API).
	// Desktop tools wrap xdotool + the sandbox's desktop_observe.py.
	// Both families register via spec-driven helpers — see browserSpecs /
	// desktopSpecs in their respective files for the declarative registry.
	mcpserver.RegisterBrowserTools(srv, bashExec, mcpserver.BrowserToolConfig{})
	mcpserver.RegisterDesktopTools(srv, bashExec, mcpserver.DesktopToolConfig{})

	// History sidecar — opt-in via PUX_HISTORY_DIR. When set, opens a sqlite
	// database at <dir>/history.sqlite and records dispatch task lifecycle +
	// agent-loop transcripts + in-loop tool calls. Fully deletable: rm the
	// history package + these 8 lines + the matching cmd/pux-history binary
	// and the server still builds + runs identically.
	var taskObs core.TaskObserver
	var chatObs core.ChatObserver
	var toolObs core.ToolObserver
	if dir := os.Getenv("PUX_HISTORY_DIR"); dir != "" {
		rec, err := history.New(dir)
		if err != nil {
			logger.Fatal("history recorder init failed", zap.String("dir", dir), zap.Error(err))
		}
		defer rec.Close()
		taskObs, chatObs, toolObs = rec, rec, rec
		logger.Info("history recorder enabled", zap.String("dir", dir))
	}

	// Dispatch surface (org → agent loop) is opt-in via PUX_LLM_API_KEY.
	// When enabled, three new MCP tools land: dispatch_task, get_task_status,
	// list_orgs. The runtime re-uses the sandbox tools registered above as
	// the agent's in-loop catalog.
	var taskStore *mcpserver.TaskStore
	if apiKey := os.Getenv("PUX_LLM_API_KEY"); apiKey != "" {
		provider, err := agent.NewProvider(agent.ProviderConfig{
			APIKey:    apiKey,
			BaseURL:   os.Getenv("PUX_LLM_BASE_URL"),
			Model:     os.Getenv("PUX_LLM_MODEL"),
			MaxTokens: envOrZero("PUX_LLM_MAX_TOKENS"),
		})
		if err != nil {
			logger.Fatal("dispatch provider init failed", zap.Error(err))
		}
		taskStore = mcpserver.NewTaskStore(taskObs)
		loader := org.NewLoader(*projectPath)
		rt := newDispatchRuntime(provider, loader, taskStore, srv.Tools(), chatObs, toolObs, logger)

		srv.RegisterTool(mcpserver.NewDispatchTool(taskStore, rt))
		srv.RegisterTool(mcpserver.NewTaskStatusTool(taskStore))
		srv.RegisterTool(mcpserver.NewListOrgsTool(&orgLister{loader: loader}))

		logger.Info("dispatch surface enabled",
			zap.String("model", provider.ModelName()),
			zap.String("orgs_root", *projectPath+"/orgs"))
	} else {
		logger.Info("dispatch surface disabled (set PUX_LLM_API_KEY to enable)")
	}

	// 6. HTTP server.
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Minute, // tools can be slow (long bash)
		IdleTimeout:       120 * time.Second,
	}

	// 7. Graceful shutdown. Sequence: signal → cancel tasks → destroy sandbox
	// → stop HTTP → remove PID file → exit. Sandbox destroy happens BEFORE
	// HTTP shutdown so main() doesn't race-exit and orphan the goroutine
	// mid-destroy. The done channel blocks main until the teardown completes.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sig := <-shutdown
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))

		// 1. Cancel in-flight dispatch tasks.
		if taskStore != nil {
			taskStore.Shutdown()
		}

		// 2. Destroy the sandbox (slow operation).
		if !*keepAlive {
			sbCtx, sbCancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := mgr.DestroySandbox(sbCtx, sb.ID); err != nil {
				logger.Warn("sandbox destroy failed (non-fatal)", zap.Error(err))
			} else {
				logger.Info("sandbox destroyed cleanly", zap.String("sandbox_id", sb.ID))
			}
			sbCancel()
		}

		// 3. Stop the HTTP server.
		httpCtx, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := httpSrv.Shutdown(httpCtx); err != nil {
			logger.Warn("http shutdown errored (non-fatal)", zap.Error(err))
		}
		httpCancel()

		// 4. Remove the PID file (supervisor bookkeeping).
		if err := removePIDFile(pidPath); err != nil && !os.IsNotExist(err) {
			logger.Warn("pid file remove failed (non-fatal)", zap.Error(err))
		}
	}()

	logger.Info(fmt.Sprintf("MCP server listening on http://%s", *addr))
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("listen failed", zap.Error(err))
		}
	}()
	<-done
	logger.Info("shutdown complete")
}

// runStart daemonizes `mcpserver run` with the same flags, waits for the
// PID file to appear (readiness signal), then returns. Refuses if a server
// is already running for this project unless --force stops it first.
func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	addr := fs.String("addr", envOrDefault("PUX_MCP_ADDR", "127.0.0.1:9987"), "Listen address")
	projectPath := fs.String("project", envOrDefault("PUX_PROJECT_PATH", ""), "Project directory")
	sandboxID := fs.String("sandbox-id", envOrDefault("PUX_SANDBOX_ID", "mcp-default"), "Sandbox ID")
	keepAlive := fs.Bool("keep-alive", false, "Leave sandbox running on exit")
	logPath := fs.String("log", envOrDefault("PUX_MCP_LOG", ""), "Log file path (- = stderr, empty = discard)")
	force := fs.Bool("force", false, "Stop a running server before starting")
	_ = fs.Parse(args)

	*projectPath = resolveProjectPath(*projectPath)
	pidPath := resolvePIDFile(*projectPath)

	// Stale-or-live check on existing PID file.
	if entry, err := readPIDFile(pidPath); err == nil {
		if isProcessAlive(entry.PID) {
			if !*force {
				fmt.Fprintf(os.Stderr, "server already running (PID %d, addr %s)\n", entry.PID, entry.Addr)
				fmt.Fprintf(os.Stderr, "use 'mcpserver stop' first, or pass --force\n")
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "--force: stopping existing server (PID %d)...\n", entry.PID)
			if err := stopProcess(entry.PID, 10*time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "force-stop failed: %v\n", err)
				os.Exit(1)
			}
			_ = removePIDFile(pidPath)
		} else {
			// Stale — clean up.
			_ = removePIDFile(pidPath)
		}
	}

	// Build the `run` arg list to pass through.
	runArgs := []string{"run",
		"--addr", *addr,
		"--project", *projectPath,
		"--sandbox-id", *sandboxID,
	}
	if *keepAlive {
		runArgs = append(runArgs, "--keep-alive")
	}

	pid, err := daemonize(runArgs, *logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start: %v\n", err)
		os.Exit(1)
	}

	// Wait for the child to write its PID file (sandbox-ready signal).
	// Sandbox boot dominates — give it up to 90s.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		entry, err := readPIDFile(pidPath)
		if err == nil && entry.PID == pid {
			fmt.Printf("started: PID %d, addr %s, project %s\n", pid, *addr, *projectPath)
			fmt.Printf("logs: ")
			switch *logPath {
			case "":
				fmt.Printf("(discarded — pass --log <path> to capture)\n")
			case "-":
				fmt.Printf("(stderr, captured by parent)\n")
			default:
				fmt.Printf("%s\n", *logPath)
			}
			fmt.Printf("pid file: %s\n", pidPath)
			return
		}
		// If the child died before writing the PID file, exit fast.
		if !isProcessAlive(pid) {
			fmt.Fprintf(os.Stderr, "child exited before writing PID file — check logs\n")
			os.Exit(1)
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "timeout waiting for PID file after 90s\n")
	// Best-effort cleanup of the orphaned child.
	_ = stopProcess(pid, 5*time.Second)
	os.Exit(1)
}

// runStop reads the PID file, SIGTERMs the server, waits up to --wait
// seconds, SIGKILLs if needed. Removes the PID file.
func runStop(args []string) {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	projectPath := fs.String("project", envOrDefault("PUX_PROJECT_PATH", ""), "Project directory (PID file location)")
	waitSec := fs.Int("wait", 10, "Seconds to wait for SIGTERM before SIGKILL")
	_ = fs.Parse(args)

	*projectPath = resolveProjectPath(*projectPath)
	pidPath := resolvePIDFile(*projectPath)

	entry, err := readPIDFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "no PID file at %s — server not running?\n", pidPath)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "read pid file: %v\n", err)
		os.Exit(1)
	}
	if !isProcessAlive(entry.PID) {
		fmt.Printf("server not running (stale PID file, cleaning up)\n")
		_ = removePIDFile(pidPath)
		return
	}
	fmt.Printf("stopping PID %d (addr %s)...\n", entry.PID, entry.Addr)
	if err := stopProcess(entry.PID, time.Duration(*waitSec)*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "stop failed: %v\n", err)
		os.Exit(1)
	}
	_ = removePIDFile(pidPath)
	fmt.Printf("stopped\n")
}

// runStatus reads the PID file + reports state. With --live, also POSTs
// a ping to verify the server is actually serving (not just running).
func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	projectPath := fs.String("project", envOrDefault("PUX_PROJECT_PATH", ""), "Project directory (PID file location)")
	live := fs.Bool("live", false, "POST a ping to verify the server responds")
	_ = fs.Parse(args)

	*projectPath = resolveProjectPath(*projectPath)
	pidPath := resolvePIDFile(*projectPath)

	entry, err := readPIDFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("not running (no PID file at %s)\n", pidPath)
			return
		}
		fmt.Fprintf(os.Stderr, "read pid file: %v\n", err)
		os.Exit(1)
	}
	if !isProcessAlive(entry.PID) {
		fmt.Printf("not running (stale PID file — process %d dead)\n", entry.PID)
		return
	}
	uptime := time.Since(entry.StartedAt).Round(time.Second)
	fmt.Printf("running\n")
	fmt.Printf("  PID           %d\n", entry.PID)
	fmt.Printf("  Addr          %s\n", entry.Addr)
	fmt.Printf("  Project       %s\n", entry.Project)
	fmt.Printf("  Sandbox       %s (container %s)\n", entry.SandboxID, entry.ContainerID)
	fmt.Printf("  Started       %s (%s ago)\n", entry.StartedAt.Local().Format(time.RFC3339), uptime)
	fmt.Printf("  PID file      %s\n", pidPath)
	if *live {
		if err := pingMCP(entry.Addr); err != nil {
			fmt.Printf("  Live check    FAIL: %v\n", err)
		} else {
			fmt.Printf("  Live check    OK\n")
		}
	}
}

// pingMCP POSTs a JSON-RPC ping envelope to the server — cheap liveness
// probe that confirms the HTTP listener is actually serving, not just
// that the process exists.
func pingMCP(addr string) error {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	req, err := http.NewRequest("POST", "http://"+addr, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// resolveProjectPath absolutizes the project path. Falls back to CWD if
// empty (useful for `cd ~/code/myproject && mcpserver run`). Shared by
// all subcommands so PID file lookups are consistent.
func resolveProjectPath(p string) string {
	if p == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("--project or PUX_PROJECT_PATH required: %v", err)
		}
		p = cwd
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		log.Fatalf("resolve project path: %v", err)
	}
	return abs
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envOrZero returns the integer value of the named env var, or 0 if unset /
// unparseable. Used for optional knobs like PUX_LLM_MAX_TOKENS where zero
// means "use the provider default".
func envOrZero(key string) int {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	n, err := atoiPositive(v)
	if err != nil {
		return 0
	}
	return n
}

func atoiPositive(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a positive integer: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
