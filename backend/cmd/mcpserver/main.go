// Command mcpserver runs the Pux MCP server.
//
// Single-tenant, localhost-only. Boots a Docker sandbox, registers the
// sandbox-aware tools (bash, file_read, file_write, file_edit, file_grep,
// file_glob, python) as MCP tools, and serves JSON-RPC over HTTP on
// 127.0.0.1.
//
// Usage:
//
//	PUX_PROJECT_PATH=/path/to/workspace mcpserver
//
// Connect from Claude Desktop / Hermes / OpenClaw by adding to the client's
// MCP config:
//
//	{
//	  "mcpServers": {
//	    "pux": {
//	      "url": "http://127.0.0.1:9876"
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
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/agent"
	"github.com/auto-developer-orchestrator/backend/internal/audit"
	"github.com/auto-developer-orchestrator/backend/internal/mcpserver"
	"github.com/auto-developer-orchestrator/backend/internal/org"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
)

const version = "0.1.0-mvp"

func main() {
	addr := flag.String("addr", envOrDefault("PUX_MCP_ADDR", "127.0.0.1:9876"), "Listen address (localhost-only by default)")
	projectPath := flag.String("project", envOrDefault("PUX_PROJECT_PATH", ""), "Project directory mounted into the sandbox (required)")
	sandboxID := flag.String("sandbox-id", envOrDefault("PUX_SANDBOX_ID", "mcp-default"), "Sandbox ID")
	keepAlive := flag.Bool("keep-alive", false, "Leave sandbox running on exit (default: destroy)")
	flag.Parse()

	if *projectPath == "" {
		// Fall back to CWD if no explicit project path. Useful for `cd ~/code/myproject && mcpserver`.
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("--project or PUX_PROJECT_PATH required: %v", err)
		}
		*projectPath = cwd
	}
	abs, err := filepath.Abs(*projectPath)
	if err != nil {
		log.Fatalf("resolve project path: %v", err)
	}
	*projectPath = abs

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("pux mcp server starting",
		zap.String("version", version),
		zap.String("addr", *addr),
		zap.String("project", *projectPath),
		zap.String("sandbox_id", *sandboxID),
	)

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

	// 3. Adapters bridge the sandbox manager to the tool implementations.
	bashExec := &adapters.BashExecutor{Mgr: mgr, SandboxID: sb.ID}
	fileOps := &adapters.FileOps{Mgr: mgr, SandboxID: sb.ID}

	// 4. Build the tool registry.
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
	srv.RegisterTool(file.NewReadTool(fileOps, nil))
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
		taskStore = mcpserver.NewTaskStore()
		loader := org.NewLoader(*projectPath)
		rt := newDispatchRuntime(provider, loader, taskStore, srv.Tools(), logger)

		srv.RegisterTool(mcpserver.NewDispatchTool(taskStore, rt))
		srv.RegisterTool(mcpserver.NewTaskStatusTool(taskStore))
		srv.RegisterTool(mcpserver.NewListOrgsTool(&orgLister{loader: loader}))

		logger.Info("dispatch surface enabled",
			zap.String("model", provider.ModelName()),
			zap.String("orgs_root", *projectPath+"/orgs"))
	} else {
		logger.Info("dispatch surface disabled (set PUX_LLM_API_KEY to enable)")
	}

	// 5. HTTP server.
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Minute, // tools can be slow (long bash)
		IdleTimeout:       120 * time.Second,
	}

	// 6. Graceful shutdown. Sequence: signal → cancel tasks → destroy sandbox → stop HTTP → exit.
	// Sandbox destroy happens BEFORE HTTP shutdown so main() doesn't race-exit
	// and orphan the goroutine mid-destroy. The done channel blocks main until
	// the teardown completes.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sig := <-shutdown
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))

		// 1. Cancel in-flight dispatch tasks. Cheap; gives goroutines a chance
		//    to surface "server shutdown" status before the sandbox dies.
		if taskStore != nil {
			taskStore.Shutdown()
		}

		// 2. Destroy the sandbox (slow operation). HTTP server still serves so
		//    in-flight requests get answers — but the sandbox is gone, so
		//    they'll fail. That's fine; SIGTERM is a hard stop.
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
