package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
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
		projectRoot = "../"
	}

	// Initialize handlers
	gitOps := git.NewGitOps(logger)
	checklistHandler := handlers.NewChecklistHandler(db, logger)
	projectHandler := handlers.NewProjectHandler(db, logger, gitOps)
	aiHandler := handlers.NewAIHandler(logger)
	configHandler := handlers.NewConfigHandler(logger)
	githubHandler := handlers.NewGitHubHandler(logger)
	cliHandler := handlers.NewCLIHandler(logger, projectRoot)

	// Pi agent pool
	piPool := pi.NewPiPool(logger, 5*time.Minute)
	piHandler := handlers.NewPiHandler(piPool, db, gitOps, githubHandler, logger)

	// Sub-agent manager
	subAgentMgr := pi.NewSubAgentManager(piPool, logger,
		pi.WithSandboxManager(sandboxMgr),
	)
	subAgentHandler := handlers.NewSubAgentHandler(subAgentMgr, piPool, logger)

	// Task manager
	taskMgr := pi.NewTaskManager()
	taskHandler := handlers.NewTaskHandler(taskMgr, logger)

	// Session manager (used for session persistence in PiHandler)
	_ = pi.NewSessionManager(logger)

	// Sandbox handler
	sandboxHandler := handlers.NewSandboxHandler(sandboxMgr, logger)

	// Vision client (shared by Web Sub-Agent and Computer Use)
	litellmURL := os.Getenv("LITELLM_PROXY_URL")
	litellmKey := os.Getenv("LITELLM_MASTER_KEY")
	visionClient := browser.NewVisionClient(litellmURL, litellmKey)

	// Browser automation (Web Sub-Agent)
	var browserClient *browser.BrowserClient
	var webHandler *handlers.WebHandler
	if browserlessURL := os.Getenv("BROWSERLESS_URL"); browserlessURL != "" {
		var err error
		browserClient, err = browser.NewBrowserClient(browserlessURL, logger)
		if err != nil {
			logger.Warn("Failed to initialize browser client, web automation disabled", zap.Error(err))
		} else {
			webHandler = handlers.NewWebHandler(browserClient, visionClient, logger)
			logger.Info("Browser automation enabled", zap.String("browserless_url", browserlessURL))
		}
	}

	// Computer Use handler (CDP bridge for sandbox browser automation)
	computerUseHandler := handlers.NewComputerUseHandler(sandboxMgr, visionClient, logger)

	// Scheduler (CRON/heartbeat system)
	schedulerStorePath := os.Getenv("SCHEDULER_STORE_PATH")
	if schedulerStorePath == "" {
		schedulerStorePath = "../data/scheduler/jobs.json"
	}
	schedulerPromptSender := func(ctx context.Context, project, agentID, message, model string) (string, error) {
		// Resolve project path from database or default projects dir
		var projectPath string
		if db != nil {
			customProjects, err := db.GetCustomProjects(ctx)
			if err == nil {
				for _, p := range customProjects {
					if p.Name == project {
						projectPath = p.Path
						break
					}
				}
			}
		}
		if projectPath == "" {
			projectPath = projectRoot + "/" + project
			if _, err := os.Stat(projectPath); err != nil {
				return "", fmt.Errorf("project %s not found", project)
			}
		}

		client, err := piPool.GetOrCreateWithID(projectPath, agentID)
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
	sched := scheduler.NewScheduler(schedulerStorePath, handlers.NewSchedulerPromptSender(schedulerPromptSender), logger)

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
	artifactHandler := handlers.NewArtifactHandler(logger)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(handlers.Recoverer(logger))
	r.Use(middleware.Timeout(60 * time.Second))

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

		// Test Generation (proxied to Python or handled via LiteLLM)
		r.Post("/generate-tests", aiHandler.GenerateTests)
		r.Post("/run-tests", aiHandler.RunTests)

		// Configuration
		r.Get("/config/ai", configHandler.GetAI)
		r.Post("/config/ai", configHandler.SetAI)
		r.Get("/config/system", configHandler.GetSystem)
		r.Post("/config/system", configHandler.SetSystem)

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

			// VNC proxy — serves the sandbox desktop via noVNC
			r.HandleFunc("/vnc/{id}/*", sandboxHandler.VNCProxy)
		})

		// Browser automation (Web Sub-Agent)
		if webHandler != nil {
			r.Route("/pi/web", func(r chi.Router) {
				webHandler.RegisterRoutes(r)
			})
		}

		// Task management
		r.Route("/pi/tasks", func(r chi.Router) {
			taskHandler.RegisterRoutes(r)
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
		WriteTimeout: 60 * time.Second, // Longer for SSE
		IdleTimeout:  60 * time.Second,
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

	// Shutdown task manager
	taskMgr.Shutdown()

	// Shutdown browser client
	if browserClient != nil {
		browserClient.Shutdown()
	}

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
