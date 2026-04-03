package main

import (
	"context"
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

	// Sandbox handler
	sandboxHandler := handlers.NewSandboxHandler(sandboxMgr, logger)

	// Browser automation (Web Sub-Agent)
	var browserClient *browser.BrowserClient
	var webHandler *handlers.WebHandler
	if browserlessURL := os.Getenv("BROWSERLESS_URL"); browserlessURL != "" {
		var err error
		browserClient, err = browser.NewBrowserClient(browserlessURL, logger)
		if err != nil {
			logger.Warn("Failed to initialize browser client, web automation disabled", zap.Error(err))
		} else {
			litellmURL := os.Getenv("LITELLM_PROXY_URL")
			litellmKey := os.Getenv("LITELLM_MASTER_KEY")
			visionClient := browser.NewVisionClient(litellmURL, litellmKey)
			webHandler = handlers.NewWebHandler(browserClient, visionClient, logger)
			logger.Info("Browser automation enabled", zap.String("browserless_url", browserlessURL))
		}
	}

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
		})

		// Browser automation (Web Sub-Agent)
		if webHandler != nil {
			r.Route("/pi/web", func(r chi.Router) {
				webHandler.RegisterRoutes(r)
			})
		}
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

	// Shutdown browser client
	if browserClient != nil {
		browserClient.Shutdown()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}
