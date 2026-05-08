package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/manifest"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/models"
	"github.com/auto-developer-orchestrator/backend/internal/observability"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	"github.com/auto-developer-orchestrator/backend/internal/services"
	"github.com/auto-developer-orchestrator/backend/internal/storage"

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
	email = pux@orchestrator.local
	name = Pux Agent
`
	// Write git-credentials file if GITHUB_TOKEN is available, so git push works.
	if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
		os.WriteFile("/tmp/.git-credentials", []byte("https://pux-agent:"+ghToken+"@github.com\n"), 0600)
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
	perms.ModelConfigProvider = modelCfg.ProviderForModel

	// Connect to llama-server HTTP engine.
	// llama-server manages the model and KV cache — the Go backend sends HTTP requests
	// using OpenAI-style native tool calling for structured tool responses.
	// Model name is read from /tmp/orchestrator-model.json (written by `task model`)
	// or falls back to the model alias in config/models.json.
	llamaServerURL := os.Getenv("LLAMA_SERVER_URL")
	if llamaServerURL == "" {
		llamaServerURL = "http://localhost:8001"
	}
	modelAlias := readActiveModelAlias()
	var llamaEngine *llamaeng.LLMClient
	{
		llamaEngine = llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
			BaseURL:   llamaServerURL,
			ModelName: modelAlias,
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

	// Connect to Gemini cloud engine (optional — requires GEMINI_API_KEY env var).
	// Uses Google's OpenAI-compatible endpoint with Bearer auth.
	var geminiEngine *llamaeng.LLMClient
	if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
		geminiEngine = llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
			BaseURL:   "https://generativelanguage.googleapis.com/v1beta/openai",
			APIKey:    geminiKey,
			ModelName: "gemini-3-flash-preview",
			Logger:    logger,
		})
		if err := geminiEngine.LoadModel(); err != nil {
			logger.Warn("Gemini engine failed to initialize", zap.Error(err))
			geminiEngine = nil
		} else {
			defer geminiEngine.Close()
			logger.Info("Gemini cloud engine connected", zap.String("model", "gemini-3-flash-preview"))
		}
	}

	// Connect to the Ray cluster LLM (optional, falls back to local).
	// Cluster has Qwen3.6-27B at 30080/llm — always on GPU server.
	clusterURL := os.Getenv("CLUSTER_LLM_URL")
	if clusterURL == "" {
		clusterURL = services.HubBase() + "/llm"
	}
	var clusterEngine *llamaeng.LLMClient
	if os.Getenv("CLUSTER_LLM_DISABLE") != "true" {
		clusterEngine = llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
			BaseURL:   clusterURL,
			ModelName: "qwen3.6-27b",
			Logger:    logger,
		})
		if err := clusterEngine.LoadModel(); err != nil {
			logger.Warn("Ray cluster LLM not reachable — will not use cluster engine",
				zap.Error(err), zap.String("url", clusterURL))
			clusterEngine = nil
		} else {
			defer clusterEngine.Close()
			logger.Info("Ray cluster LLM engine connected",
				zap.String("url", clusterURL),
				zap.String("model", "qwen3.6-27b"),
			)
		}
	}

	// Connect to OpenRouter cloud engine (optional — reads from settings.json).
	// Supports DeepSeek V4 Flash and any model on OpenRouter.
	var openrouterEngine *llamaeng.LLMClient
	{
		type providerSettings struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		}
		if data, err := os.ReadFile(os.Getenv("HOME") + "/.pi/agent/settings.json"); err == nil {
			var settings struct {
				Providers map[string]providerSettings `json:"providers"`
			}
			if json.Unmarshal(data, &settings) == nil {
				if or, ok := settings.Providers["openrouter"]; ok && or.APIKey != "" {
					modelID := "deepseek/deepseek-v4-flash"
					if len(or.Models) > 0 && or.Models[0].ID != "" {
						modelID = or.Models[0].ID
					}
					openrouterEngine = llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
						BaseURL:   or.BaseURL,
						APIKey:    or.APIKey,
						ModelName: modelID,
						Logger:    logger,
					})
					if err := openrouterEngine.LoadModel(); err != nil {
						logger.Warn("OpenRouter engine failed to initialize", zap.Error(err))
						openrouterEngine = nil
					} else {
						defer openrouterEngine.Close()
						logger.Info("OpenRouter cloud engine connected", zap.String("model", modelID))
					}
				}
			}
		}
	}

	// Select the default engine: local → cluster → Gemini → OpenRouter
	activeEngine := llamaEngine
	if activeEngine == nil {
		activeEngine = clusterEngine
	}
	if activeEngine == nil {
		activeEngine = geminiEngine
	}
	if activeEngine == nil {
		activeEngine = openrouterEngine
	}
	if activeEngine == nil {
		logger.Warn("No LLM engine available — agent features disabled")
	}

	// Initialize sandbox manager
	sandboxMgr, err := sandbox.NewManager(logger)
	if err != nil {
		logger.Warn("Failed to initialize sandbox manager, running without isolation", zap.Error(err))
	}

	// Recover sandboxes from Docker containers (survives backend restarts)
	if sandboxMgr != nil {
		if err := sandboxMgr.RecoverAllSandboxes(context.Background()); err != nil {
			logger.Warn("Failed to recover sandboxes from Docker", zap.Error(err))
		}
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

	// Agent handler (orchestrator + ephemeral sub-agents via llama-server)
	puxHandler := handlers.NewPuxHandler(db, gitOps, githubHandler, logger)

	// Observability
	metrics := observability.NewMetrics()
	puxHandler.SetMetrics(metrics)
	langfuse := observability.NewLangfuseClient()
	puxHandler.SetLangfuse(langfuse)

	// Event store for session persistence (survives server restarts)
	eventStore := storage.NewEventStore(db.DB())
	puxHandler.SetEventStore(eventStore)

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

	// New architecture is the default and only path.
	puxHandler.SetSandboxOnly(sandboxMgr, computerUseHandler, x11Handler)

	// Wire llama-server HTTP engine into PuxHandler
	if activeEngine != nil {
		puxHandler.SetLlamaEngine(activeEngine, sandboxMgr, computerUseHandler, x11Handler)
		logger.Info("PuxHandler configured with LLM engine", zap.String("model", activeEngine.ModelName()))
	} else {
		// No local model, but still wire sandbox manager + computer use for cloud-only mode
		puxHandler.SetSandboxOnly(sandboxMgr, computerUseHandler, x11Handler)
	}

	// Wire Gemini cloud engine (optional)
	if geminiEngine != nil {
		puxHandler.SetGeminiEngine(geminiEngine)
	}

	// Wire Ray cluster engine (optional — fallback when local is unavailable)
	if clusterEngine != nil {
		puxHandler.SetClusterEngine(clusterEngine)
	}

	// Wire OpenRouter cloud engine (optional)
	if openrouterEngine != nil {
		puxHandler.SetOpenRouterEngine(openrouterEngine)
	}

	// MCP servers — non-fatal, tools fall back to browser-based search
	mcpMulti := mcp.NewMultiClient(logger)

	// MCP Hub endpoints — single port 30080 with path-based routing
	hubBase := os.Getenv("MCP_HUB_ENDPOINT")
	if hubBase == "" {
		hubBase = "http://100.86.69.57:30080"
	}

	webResearchClient := mcp.NewClient(hubBase+"/mcp/web", logger)
	mcpMulti.AddClient("web", webResearchClient)

	mediaAnalysisClient := mcp.NewClient(hubBase+"/mcp/media", logger)
	mcpMulti.AddClient("media", mediaAnalysisClient)

	puxHandler.SetVisionClient(visionClient)

	if mcpMulti.IsAvailable() {
		if err := mcpMulti.InitializeAll(context.Background()); err != nil {
			logger.Warn("MCP multi-client initialization had errors", zap.Error(err))
		}
		puxHandler.SetMCPMulti(mcpMulti)
		puxHandler.SetMCPClient(webResearchClient)
		logger.Info("MCP servers ready")
	} else {
		logger.Info("MCP servers not available — search/scrape will use browser fallback")
	}

	// File transfer handler (upload/download files to/from sandbox)
	fileHandler := handlers.NewFileHandler(sandboxMgr, logger)

	// Cluster services handler (LLM, TTS, ASR, Forge on Ray cluster)
	clusterHandler := handlers.NewClusterHandler(logger)

	// Direct tool execution handler (for Python SDK / external consumers)
	toolsHandler := handlers.NewToolsHandler(sandboxMgr, mcpMulti, webResearchClient, logger)

	// Scheduler (CRON/heartbeat system)
	schedulerStorePath := os.Getenv("SCHEDULER_STORE_PATH")
	if schedulerStorePath == "" {
		schedulerStorePath = "../data/scheduler/jobs.json"
	}
	// No-op sender — llama executor handles job execution directly
	sched := scheduler.NewScheduler(schedulerStorePath, nil, logger)

	// Use engine directly for scheduled jobs
	if activeEngine != nil {
		llamaExec := scheduler.NewLlamaExecutor(activeEngine, sandboxMgr, projectRoot, logger)
		llamaExec.SetMCPMulti(mcpMulti)
		sched.SetLlamaExecutor(llamaExec)
		logger.Info("Scheduler configured for direct llama engine execution")
	} else {
		// Fallback: Use isolated executor for job execution
		isolatedExec, err := scheduler.NewIsolatedExecutor(projectRoot, logger)
		if err != nil {
			logger.Warn("Failed to create isolated executor", zap.Error(err))
		} else {
			runLogMgr, err := scheduler.NewRunLogManager("")
			if err != nil {
				logger.Warn("Failed to create run log manager", zap.Error(err))
			}
			sched.SetIsolatedExecutor(isolatedExec, runLogMgr, projectRoot)
		}
	}

	if err := sched.Start(context.Background()); err != nil {
		logger.Warn("Failed to start scheduler", zap.Error(err))
	}
	schedulerHandler := handlers.NewSchedulerHandler(sched, logger)
	projectHandler.SetScheduler(sched)

	// Wire sandbox manager for auto-creating sandboxes on project add
	projectHandler.SetSandboxManager(sandboxMgr)

	// Wire sandbox initializer for manifest sandbox auto-setup (files, pip, env)
	sandboxIniter := handlers.NewSandboxInitializer(sandboxMgr, logger)
	projectHandler.SetSandboxInitializer(sandboxIniter)

	// Re-initialize sandboxes for existing projects that have sandbox config
	if projects, err := db.GetCustomProjects(context.Background()); err == nil {
		for _, p := range projects {
			if mf, err := manifest.LoadManifest(p.Path); err == nil && mf != nil && mf.Sandbox != nil {
				result := sandboxIniter.InitIfSandboxExists(context.Background(), p.Name, mf.Sandbox, p.Path)
				if !result.SandboxNotFound {
					logger.Info("Re-initialized sandbox from manifest on startup",
						zap.String("project", p.Name),
						zap.Int("files", result.FilesUploaded),
						zap.Int("pip", result.PipPackagesInstalled),
					)
				}
			}
		}
	}

	// Artifacts handler (plans, todos, notes from agents)
	artifactHandler := handlers.NewArtifactHandler(db, logger)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(handlers.Recoverer(logger))
	r.Use(middleware.Timeout(2 * time.Hour)) // Deep research can take hours; SSE streams must not be cut short

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Prometheus metrics
	r.Get("/metrics", metrics.HTTPHandler().ServeHTTP)

	// API Routes
	r.Route("/api", func(r chi.Router) {
		// Direct tool execution (Python SDK / external consumers)
		r.Post("/tools/exec", toolsHandler.ExecTool)
		r.Get("/tools", toolsHandler.ToolsList)

		// Health check — reports component status
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			status := map[string]string{
				"status": "ok",
			}
			if activeEngine != nil {
				if err := activeEngine.CheckHealth(); err != nil {
					status["llm"] = "degraded: " + err.Error()
				} else {
					status["llm"] = "healthy"
				}
			} else {
				status["llm"] = "unavailable"
			}
			if sandboxMgr != nil {
				status["sandbox"] = "available"
			} else {
				status["sandbox"] = "unavailable"
			}
			status["version"] = "0.2.0"

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
		})

		// Projects
		r.Get("/projects", projectHandler.List)
		r.Post("/projects/add", projectHandler.Add)
		r.Post("/projects/register", projectHandler.Add) // Alias: same handler for /register
		r.Post("/clone", projectHandler.Clone)
		r.Post("/branch/checkout", projectHandler.CheckoutBranch)
		r.Get("/branch", projectHandler.GetBranch)
		r.Get("/projects/{name}/manifest", projectHandler.GetManifest)

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
		r.Get("/config/providers", configHandler.GetProviders)
		r.Put("/config/providers", configHandler.SetProviderKey)
		r.Get("/config/agent", configHandler.GetAgent)
		r.Put("/config/agent", configHandler.SetAgent)

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

		// Pux Agent
		r.Route("/pux", func(r chi.Router) {
			puxHandler.RegisterRoutes(r)
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

			// Readiness check
			r.Get("/{id}/ready", sandboxHandler.IsReady)

			// VNC health check (is websockify listening?)
			r.Get("/{id}/vnc-health", sandboxHandler.VNCReadinessCheck)

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

			// VNC connection stats
			r.Get("/vnc-stats", sandboxHandler.VNCStats)
		})

		// Artifacts (plans, todos, notes from agents)
		r.Route("/pux/artifacts", func(r chi.Router) {
			artifactHandler.RegisterRoutes(r)
		})

		// Scheduler (CRON/recurring jobs)
		r.Route("/scheduler", func(r chi.Router) {
			schedulerHandler.RegisterRoutes(r)
		})

		// Cluster services (LLM, TTS, ASR, Forge on Ray cluster)
		r.Route("/cluster", func(r chi.Router) {
			r.Get("/status", clusterHandler.ClusterStatus)        // health of all services
			r.Get("/tts", clusterHandler.TTSServices)             // list TTS backends
			r.Post("/tts/synthesize", clusterHandler.SynthesizeSpeech) // text → speech
			r.Get("/asr", clusterHandler.ASRStatus)               // ASR health
			r.Post("/asr/transcribe", clusterHandler.TranscribeAudio) // audio → text
			r.Get("/forge", clusterHandler.ForgeStatus)           // Forge router health
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

	// Shutdown computer use handler
	computerUseHandler.Shutdown()

	// Close all active VNC proxy connections
	sandboxHandler.CleanupVNCConnections()

	// Shutdown scheduler
	sched.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}

// readActiveModelAlias determines the model alias to send in API requests.
// Priority: /tmp/orchestrator-model.json (written by `task model`) →
// config/models.json current field → hardcoded fallback "gemma-4-26b".
func readActiveModelAlias() string {
	// Fast path: runtime file from `task model`
	if data, err := os.ReadFile("/tmp/orchestrator-model.json"); err == nil {
		var m struct {
			Alias string `json:"alias"`
		}
		if json.Unmarshal(data, &m) == nil && m.Alias != "" {
			return m.Alias
		}
	}

	// Config path: read current profile from config/models.json
	if root := os.Getenv("PROJECT_ROOT"); root != "" {
		cfgPath := filepath.Join(root, "config", "models.json")
		if data, err := os.ReadFile(cfgPath); err == nil {
			var cfg struct {
				Current string `json:"current"`
				Models  map[string]struct {
					Alias string `json:"alias"`
				} `json:"models"`
			}
			if json.Unmarshal(data, &cfg) == nil && cfg.Current != "" {
				if m, ok := cfg.Models[cfg.Current]; ok && m.Alias != "" {
					return m.Alias
				}
			}
		}
	}

	return "gemma-4-26b"
}
