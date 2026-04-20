package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/models"
	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/auto-developer-orchestrator/backend/internal/storage"

	"github.com/auto-developer-orchestrator/backend/internal/browser"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

func main() {
	// Allow git operations on bind-mounted repos with different ownership.
	// Host ~/.gitconfig is mounted read-only, so write to a separate config.
	gitconfig := `[safe]
	directory = *
[user]
	email = pi@orchestrator.local
	name = Pi Agent
`
	// Write git-credentials file if GITHUB_TOKEN is available, so git push works.
	if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
		os.WriteFile("/tmp/.git-credentials", []byte("https://pi-agent:"+ghToken+"@github.com\n"), 0600)
		gitconfig += `[credential]
	helper = store --file /tmp/.git-credentials
`
	}
	tmpConfig := "/tmp/.gitconfig"
	os.WriteFile(tmpConfig, []byte(gitconfig), 0644)
	os.Setenv("GIT_CONFIG_GLOBAL", tmpConfig)

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Load two-model configuration (main model + tool model)
	modelCfg, err := models.LoadModelConfig(logger)
	if err != nil {
		logger.Warn("Failed to load model config, using defaults", zap.Error(err))
	}

	// Wire model config into pi package for provider resolution
	pi.ModelConfigProvider = modelCfg.ProviderForModel

	// Set TOOL_MODEL env var so Pi extensions (cron-tool, etc.) can read it
	os.Setenv("TOOL_MODEL", modelCfg.ToolModel().ModelId)

	// Connect to llama-server HTTP engine.
	// llama-server manages the model and KV cache — the Go backend sends HTTP requests
	// using OpenAI-style native tool calling for structured tool responses.
	llamaServerURL := os.Getenv("LLAMA_SERVER_URL")
	if llamaServerURL == "" {
		llamaServerURL = "http://localhost:8001"
	}
	var llamaEngine *llamaeng.HTTPEngine
	{
		llamaEngine = llamaeng.NewHTTPEngine(llamaeng.HTTPEngineConfig{
			BaseURL:   llamaServerURL,
			ModelName: "gemma-4-26b",
			Logger:    logger,
		})
		if err := llamaEngine.LoadModel(); err != nil {
			logger.Warn("llama-server not reachable — agent features disabled, sandbox/API only",
				zap.Error(err), zap.String("url", llamaServerURL))
			llamaEngine = nil
		} else {
			defer llamaEngine.Close()
			// Warm up CUDA kernels on the server side
			if err := llamaEngine.WarmUp(); err != nil {
				logger.Warn("Warm-up request failed (first prompt may be slow)", zap.Error(err))
			}
			logger.Info("llama-server HTTP engine connected",
				zap.String("url", llamaServerURL),
			)
		}
	}

	// Initialize sandbox manager
	sandboxMgr, err := sandbox.NewManager(logger)
	if err != nil {
		logger.Warn("Failed to initialize sandbox manager, running without isolation", zap.Error(err))
	}

	// Initialize database
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "../data/orchestrator.db"
	}
	// Ensure data directory exists
	if dbURL != ":memory:" {
		dbDir := dbURL[:max(0, len(dbURL)-len("/orchestrator.db"))]
		if dbDir != "" {
			os.MkdirAll(dbDir, 0755)
		}
	}
	db, err := storage.NewDatabase(dbURL)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer db.Close()

	// Project root for CLI commands and file access
	projectRoot := os.Getenv("PROJECT_ROOT")
	if projectRoot == "" {
		projectRoot, _ = os.Getwd()
		// Default to parent of the go-backend directory
		projectRoot = projectRoot + "/.."
	}
	// Ensure absolute path (Docker bind mounts and other consumers require it)
	if absRoot, err := filepath.Abs(projectRoot); err == nil {
		projectRoot = absRoot
	}
	os.Setenv("PROJECT_ROOT", projectRoot)

	// Initialize handlers
	gitOps := git.NewGitOps(logger)
	checklistHandler := handlers.NewChecklistHandler(db, logger)
	projectHandler := handlers.NewProjectHandler(db, logger, gitOps)
	githubTokenStore := handlers.NewGitHubTokenStore()
	configHandler := handlers.NewConfigHandler(logger, githubTokenStore, modelCfg, db)
	githubHandler := handlers.NewGitHubHandler(logger, githubTokenStore)
	cliHandler := handlers.NewCLIHandler(logger, projectRoot)

	// Pi agent pool
	piPool := pi.NewPiPool(logger, 5*time.Minute)
	piHandler := handlers.NewPiHandler(piPool, db, gitOps, githubHandler, logger)

	// Sub-agent manager
	subAgentMgr := pi.NewSubAgentManager(piPool, logger,
		pi.WithSandboxManager(sandboxMgr),
	)
	subAgentHandler := handlers.NewSubAgentHandler(subAgentMgr, piPool, logger, modelCfg)

	// Sandbox handler
	sandboxHandler := handlers.NewSandboxHandler(sandboxMgr, logger)

	// Vision client — defaults to llama.cpp at localhost:8001
	visionURL := os.Getenv("LITELLM_PROXY_URL") // optional override
	visionKey := os.Getenv("LITELLM_MASTER_KEY")
	visionClient := browser.NewVisionClient(visionURL, visionKey, modelCfg)

	// Computer Use handler (CDP bridge for sandbox browser automation)
	computerUseHandler := handlers.NewComputerUseHandler(sandboxMgr, visionClient, logger)

	// X11 handler (xdotool-based desktop automation for native apps)
	x11Handler := handlers.NewX11Handler(sandboxMgr, logger)

	// Wire llama-server HTTP engine into PiHandler
	if llamaEngine != nil {
		piHandler.SetLlamaEngine(llamaEngine, sandboxMgr, computerUseHandler, x11Handler)
		logger.Info("PiHandler configured for llama-server HTTP mode with computer use")
	}

	// File transfer handler (upload/download files to/from sandbox)
	fileHandler := handlers.NewFileHandler(sandboxMgr, logger)

	// Scheduler (CRON/heartbeat system)
	schedulerStorePath := os.Getenv("SCHEDULER_STORE_PATH")
	if schedulerStorePath == "" {
		schedulerStorePath = "../data/scheduler/jobs.json"
	}
	schedSender := handlers.NewSchedulerPromptSenderAdapter(piPool, db, projectRoot)
	sched := scheduler.NewScheduler(schedulerStorePath, schedSender, logger)

	// Phase 1: Set up isolated executor for job execution
	isolatedExec, err := scheduler.NewIsolatedExecutor(projectRoot, logger)
	if err != nil {
		logger.Warn("Failed to create isolated executor, falling back to main agent", zap.Error(err))
	} else {
		runLogMgr, err := scheduler.NewRunLogManager("")
		if err != nil {
			logger.Warn("Failed to create run log manager", zap.Error(err))
		}
		sched.SetIsolatedExecutor(isolatedExec, runLogMgr, projectRoot)

		// Phase 4: Set up session delivery (inject job output into main agent)
		sched.SetSessionInjector(func(project, agentID, text string) error {
			client := piPool.GetWithID(projectRoot+"/"+project, agentID)
			if client == nil {
				// No active session — log it
				logger.Info("no active session for delivery",
					zap.String("project", project),
					zap.String("agentId", agentID),
				)
				return nil
			}
			return client.SendPrompt(text, "", "")
		})
	}

	if err := sched.Start(context.Background()); err != nil {
		logger.Warn("Failed to start scheduler", zap.Error(err))
	}
	schedulerHandler := handlers.NewSchedulerHandler(sched, logger)

	// Artifacts handler (plans, todos, notes from agents)
	artifactHandler := handlers.NewArtifactHandler(db, logger)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(handlers.Recoverer(logger))
	r.Use(middleware.Timeout(10 * time.Minute)) // Long timeout for SSE streaming (agent conversations can take minutes)

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// API Routes
	r.Route("/api", func(r chi.Router) {
		// Health check
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("OK"))
		})

		// Projects
		r.Get("/projects", projectHandler.List)
		r.Post("/projects/add", projectHandler.Add)
		r.Post("/projects/register", projectHandler.Add) // Alias: same handler for /register
		r.Post("/clone", projectHandler.Clone)
		r.Post("/branch/checkout", projectHandler.CheckoutBranch)
		r.Get("/branch", projectHandler.GetBranch)

		// Status
		r.Get("/status", projectHandler.GetStatus)

		// Checklist
		r.Get("/checklist", checklistHandler.Get)
		r.Post("/checklist/update", checklistHandler.Update)
		r.Post("/ai/agent-checklist", checklistHandler.GenerateChecklistStream) // SSE streaming

		// Merge
		r.Post("/merge", checklistHandler.Merge)

		// Configuration
		r.Get("/config/ai", configHandler.GetAI)
		r.Post("/config/ai", configHandler.SetAI)
		r.Get("/config/system", configHandler.GetSystem)
		r.Post("/config/system", configHandler.SetSystem)
		r.Get("/config/models", configHandler.GetModels)
		r.Put("/config/models", configHandler.SetModels)

		// GitHub integration
		r.Get("/github/user", configHandler.GetGitHubUser)
		r.Post("/config/github", configHandler.ConnectGitHub)
		r.Get("/github/repos", githubHandler.GetRepos)
		r.Get("/github/prs", githubHandler.GetPRs)
		r.Get("/github/stats", githubHandler.GetStats)
		r.Get("/github/branches", githubHandler.GetBranches)
		r.Get("/github/activity", githubHandler.GetActivity)

		// Settings
		r.Post("/settings/mode", projectHandler.SetMode)
		r.Get("/config/project", configHandler.GetProjectSettings)
		r.Put("/config/project", configHandler.SetProjectSettings)

		// CLI Commands (safe, sandboxed)
		r.Get("/cli/commands", cliHandler.ListAllowedCommands)
		r.Post("/cli/execute", cliHandler.ExecuteCommand)
		r.Get("/cli/cat", cliHandler.ReadFile)
		r.Get("/cli/ls", cliHandler.ListDirectory)

		// Pi Coding Agent
		r.Route("/pi", func(r chi.Router) {
			piHandler.RegisterRoutes(r)

			// Sub-agent routes
			r.Route("/subagent", func(r chi.Router) {
				subAgentHandler.RegisterRoutes(r)
			})
		})

		// Sandbox management (OpenShell)
		r.Route("/sandbox", func(r chi.Router) {
			r.Post("/", sandboxHandler.CreateSandbox)
			r.Get("/", sandboxHandler.ListSandboxes)
			r.Get("/{id}", sandboxHandler.GetSandbox)
			r.Delete("/{id}", sandboxHandler.DestroySandbox)
			r.Post("/{id}/exec", sandboxHandler.ExecCommand)

			// Browser Mode (lightweight, CDP only)
			r.Post("/{id}/browser-mode", sandboxHandler.EnableBrowserMode)

			// Desktop Mode (heavy, VNC + Xvfb)
			r.Post("/{id}/desktop-mode", sandboxHandler.EnableDesktopMode)

			// Disable any mode
			r.Delete("/{id}/mode", sandboxHandler.DisableMode)

			// Get viewer URLs
			r.Get("/{id}/viewer", sandboxHandler.GetDesktopViewer)

			// Computer Use (CDP bridge for sandbox browser automation)
			r.Route("/{id}/computer-use", func(r chi.Router) {
				computerUseHandler.RegisterRoutes(r)
			})

			// X11 Desktop Automation (xdotool for native apps)
			r.Route("/{id}/x11", func(r chi.Router) {
				x11Handler.RegisterRoutes(r)
			})

			// File Transfer (upload/download files to/from sandbox)
			r.Route("/{id}/files", func(r chi.Router) {
				fileHandler.RegisterRoutes(r)
			})

			// VNC proxy — serves the sandbox desktop via noVNC
			r.HandleFunc("/vnc/{id}/*", sandboxHandler.VNCProxy)
		})

		// Artifacts (plans, todos, notes from agents)
		r.Route("/pi/artifacts", func(r chi.Router) {
			artifactHandler.RegisterRoutes(r)
		})

		// Scheduler (CRON/recurring jobs)
		r.Route("/scheduler", func(r chi.Router) {
			schedulerHandler.RegisterRoutes(r)
		})
	})

	// Serve static files (React frontend)
	r.Handle("/*", http.FileServer(http.Dir("../dist")))

	// Server setup
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "3847"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // No write timeout — SSE streams can run for minutes
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		logger.Info("Starting server", zap.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Shutdown Pi agent pool
	piPool.Shutdown()

	// Shutdown sub-agent manager
	subAgentMgr.Shutdown()

	// Shutdown computer use handler
	computerUseHandler.Shutdown()

	// Shutdown scheduler
	sched.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}
