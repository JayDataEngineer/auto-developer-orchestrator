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
	"github.com/auto-developer-orchestrator/backend/internal/engines"
	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
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

// App assembles and runs the orchestrator server.
type App struct {
	logger  *zap.Logger
	engines *engines.Engines
	db      *storage.Database
	router  chi.Router
	server  *http.Server

	// Handlers
	puxHandler         *handlers.PuxHandler
	sandboxHandler     *handlers.SandboxHandler
	computerUseHandler *handlers.ComputerUseHandler
	sched              *scheduler.Scheduler
	promPusher         *observability.MetricsPusher
}

// NewApp initializes all components and assembles the application.
func NewApp() *App {
	a := &App{}

	a.initGit()
	a.initLogger()
	a.initEngines()
	a.initDatabase()
	a.initProjectRoot()
	a.initScheduler()
	a.initHandlers()
	a.initMCP()

	return a
}

// Run starts the server and blocks until shutdown.
func (a *App) Run() {
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "3847"
	}

	a.server = &http.Server{
		Addr:         ":" + port,
		Handler:      a.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // No write timeout — SSE streams can run for minutes
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		a.logger.Info("Starting server", zap.String("port", port))
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	a.logger.Info("Shutting down server...")
	a.shutdown()
}

func (a *App) shutdown() {
	if a.computerUseHandler != nil {
		a.computerUseHandler.Shutdown()
	}
	if a.sandboxHandler != nil {
		a.sandboxHandler.CleanupVNCConnections()
	}
	if a.sched != nil {
		a.sched.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.server.Shutdown(ctx); err != nil {
		a.logger.Fatal("Server forced to shutdown", zap.Error(err))
	}
	a.logger.Info("Server stopped")
}

// ── Component initialization ──────────────────────────────────────────

func (a *App) initGit() {
	gitconfig := `[safe]
directory = *
[user]
	email = pux@orchestrator.local
	name = Pux Agent
`
	if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
		os.WriteFile("/tmp/.git-credentials", []byte("https://pux-agent:"+ghToken+"@github.com\n"), 0600)
		gitconfig += `[credential]
helper = store --file /tmp/.git-credentials
`
	}
	tmpConfig := "/tmp/.gitconfig"
	os.WriteFile(tmpConfig, []byte(gitconfig), 0644)
	os.Setenv("GIT_CONFIG_GLOBAL", tmpConfig)
}

func (a *App) initLogger() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	a.logger = logger
}

func (a *App) initEngines() {
	a.engines = engines.NewEngines(a.logger)
}

func (a *App) initDatabase() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "../data/orchestrator.db"
	}
	if dbURL != ":memory:" {
		dbDir := dbURL[:max(0, len(dbURL)-len("/orchestrator.db"))]
		if dbDir != "" {
			os.MkdirAll(dbDir, 0755)
		}
	}
	db, err := storage.NewDatabase(dbURL)
	if err != nil {
		a.logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	a.db = db
}

func (a *App) initProjectRoot() {
	projectRoot := os.Getenv("PROJECT_ROOT")
	if projectRoot == "" {
		projectRoot, _ = os.Getwd()
		projectRoot = projectRoot + "/.."
	}
	if absRoot, err := filepath.Abs(projectRoot); err == nil {
		projectRoot = absRoot
	}
	os.Setenv("PROJECT_ROOT", projectRoot)
}

func (a *App) initHandlers() {
	logger := a.logger

	// Model config
	modelCfg, err := models.LoadModelConfig(logger)
	if err != nil {
		logger.Warn("Failed to load model config, using defaults", zap.Error(err))
	}
	perms.ModelConfigProvider = modelCfg.ProviderForModel

	// Core handlers
	gitOps := git.NewGitOps(logger)
	checklistHandler := handlers.NewChecklistHandler(a.db, logger)
	projectHandler := handlers.NewProjectHandler(a.db, logger, gitOps)
	githubTokenStore := handlers.NewGitHubTokenStore()
	configHandler := handlers.NewConfigHandler(logger, githubTokenStore, modelCfg, a.db)
	githubHandler := handlers.NewGitHubHandler(logger, githubTokenStore)
	cliHandler := handlers.NewCLIHandler(logger, os.Getenv("PROJECT_ROOT"))

	// Pux handler
	a.puxHandler = handlers.NewPuxHandler(a.db, gitOps, githubHandler, logger)

	// Observability
	metrics := observability.NewMetrics()
	a.puxHandler.SetMetrics(metrics)
	langfuse := observability.NewLangfuseClient()
	a.puxHandler.SetLangfuse(langfuse)

	promPusher := observability.NewMetricsPusher(metrics.Registry(), logger)
	promPusher.Start()
	a.promPusher = promPusher

	// Infisical secrets
	if infisical := services.NewInfisicalClient(); infisical != nil {
		logger.Info("Infisical client connected")
		infisical.ResolveEnvVars("", "dev", map[string]string{
			"S3_ACCESS_KEY": "S3_ACCESS_KEY",
			"S3_SECRET_KEY": "S3_SECRET_KEY",
		})
	}

	// Event store
	eventStore := storage.NewEventStore(a.db.DB(), a.db.Dialect())
	a.puxHandler.SetEventStore(eventStore)

	// Sandbox
	sandboxMgr, err := sandbox.NewManager(logger)
	if err != nil {
		logger.Warn("Failed to initialize sandbox manager, running without isolation", zap.Error(err))
	}
	if sandboxMgr != nil {
		sandboxMgr.RecoverAllSandboxes(context.Background())
	}
	a.sandboxHandler = handlers.NewSandboxHandler(sandboxMgr, logger)

	// Vision
	visionURL := os.Getenv("LITELLM_PROXY_URL")
	visionKey := os.Getenv("LITELLM_MASTER_KEY")
	visionClient := browser.NewVisionClient(visionURL, visionKey, modelCfg)

	// Computer Use + X11
	a.computerUseHandler = handlers.NewComputerUseHandler(sandboxMgr, visionClient, logger)
	x11Handler := handlers.NewX11Handler(sandboxMgr, logger)

	// Wire engines
	a.puxHandler.SetSandboxOnly(sandboxMgr, a.computerUseHandler, x11Handler)
	if a.engines.Active != nil {
		a.puxHandler.SetLlamaEngine(a.engines.Active, sandboxMgr, a.computerUseHandler, x11Handler)
		logger.Info("PuxHandler configured with LLM engine", zap.String("model", a.engines.Active.ModelName()))
	}
	if a.engines.Gemini != nil {
		a.puxHandler.SetGeminiEngine(a.engines.Gemini)
	}
	if a.engines.Cluster != nil {
		a.puxHandler.SetClusterEngine(a.engines.Cluster)
	}
	if a.engines.OpenRouter != nil {
		a.puxHandler.SetOpenRouterEngine(a.engines.OpenRouter)
	}
	a.puxHandler.SetVisionClient(visionClient)

	// Other handlers
	fileHandler := handlers.NewFileHandler(sandboxMgr, logger)
	clusterHandler := handlers.NewClusterHandler(logger)
	toolsHandler := handlers.NewToolsHandler(sandboxMgr, nil, nil, logger) // MCP wired later
	artifactHandler := handlers.NewArtifactHandler(a.db, logger)

	// Project handler setup
	sandboxIniter := handlers.NewSandboxInitializer(sandboxMgr, logger)
	projectHandler.SetScheduler(a.sched)
	projectHandler.SetSandboxManager(sandboxMgr)
	projectHandler.SetSandboxInitializer(sandboxIniter)

	// Re-init sandboxes from manifests
	if projects, err := a.db.GetCustomProjects(context.Background()); err == nil {
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

	// Store references for router + scheduler wiring
	a.router = a.buildRouter(
		metrics, projectHandler, checklistHandler, configHandler,
		githubHandler, cliHandler, toolsHandler, fileHandler,
		clusterHandler, artifactHandler,
	)
}

func (a *App) initMCP() {
	mcpMulti := mcp.NewMultiClient(a.logger)

	hubBase := os.Getenv("MCP_HUB_ENDPOINT")
	if hubBase == "" {
		hubBase = "http://100.86.69.57:30080"
	}

	webResearchClient := mcp.NewClient(hubBase+"/mcp/web", a.logger)
	mcpMulti.AddClient("web", webResearchClient)

	mediaAnalysisClient := mcp.NewClient(hubBase+"/mcp/media", a.logger)
	mcpMulti.AddClient("media", mediaAnalysisClient)

	if mcpMulti.IsAvailable() {
		if err := mcpMulti.InitializeAll(context.Background()); err != nil {
			a.logger.Warn("MCP multi-client initialization had errors", zap.Error(err))
		}
		a.puxHandler.SetMCPMulti(mcpMulti)
		a.puxHandler.SetMCPClient(webResearchClient)
		a.logger.Info("MCP servers ready")
	} else {
		a.logger.Info("MCP servers not available — search/scrape will use browser fallback")
	}
}

func (a *App) initScheduler() {
	storePath := os.Getenv("SCHEDULER_STORE_PATH")
	if storePath == "" {
		storePath = "../data/scheduler/jobs.json"
	}

	a.sched = scheduler.NewScheduler(storePath, nil, a.logger)

	if a.engines.Active != nil {
		llamaExec := scheduler.NewLlamaExecutor(a.engines.Active, nil, os.Getenv("PROJECT_ROOT"), a.logger)
		// MCP and sandbox are wired after initMCP; the scheduler executor accesses them via the pux handler
		a.sched.SetLlamaExecutor(llamaExec)
		a.logger.Info("Scheduler configured for direct llama engine execution")
	} else {
		isolatedExec, err := scheduler.NewIsolatedExecutor(os.Getenv("PROJECT_ROOT"), a.logger)
		if err != nil {
			a.logger.Warn("Failed to create isolated executor", zap.Error(err))
		} else {
			runLogMgr, err := scheduler.NewRunLogManager("")
			if err != nil {
				a.logger.Warn("Failed to create run log manager", zap.Error(err))
			}
			a.sched.SetIsolatedExecutor(isolatedExec, runLogMgr, os.Getenv("PROJECT_ROOT"))
		}
	}

	if err := a.sched.Start(context.Background()); err != nil {
		a.logger.Warn("Failed to start scheduler", zap.Error(err))
	}
}

// ── Router ────────────────────────────────────────────────────────────

func (a *App) buildRouter(
	metrics *observability.Metrics,
	projectHandler *handlers.ProjectHandler,
	checklistHandler *handlers.ChecklistHandler,
	configHandler *handlers.ConfigHandler,
	githubHandler *handlers.GitHubHandler,
	cliHandler *handlers.CLIHandler,
	toolsHandler *handlers.ToolsHandler,
	fileHandler *handlers.FileHandler,
	clusterHandler *handlers.ClusterHandler,
	artifactHandler *handlers.ArtifactHandler,
) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(handlers.Recoverer(a.logger))
	r.Use(middleware.Timeout(2 * time.Hour))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/metrics", metrics.HTTPHandler().ServeHTTP)

	r.Route("/api", func(r chi.Router) {
		r.Post("/tools/exec", toolsHandler.ExecTool)
		r.Get("/tools", toolsHandler.ToolsList)

		// Health check
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			status := map[string]string{"status": "ok"}
			if a.engines.Active != nil {
				if err := a.engines.Active.CheckHealth(); err != nil {
					status["llm"] = "degraded: " + err.Error()
				} else {
					status["llm"] = "healthy"
				}
			} else {
				status["llm"] = "unavailable"
			}
			status["version"] = "0.2.0"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
		})

		// Projects
		r.Get("/projects", projectHandler.List)
		r.Post("/projects/add", projectHandler.Add)
		r.Post("/projects/register", projectHandler.Add)
		r.Post("/clone", projectHandler.Clone)
		r.Post("/branch/checkout", projectHandler.CheckoutBranch)
		r.Get("/branch", projectHandler.GetBranch)
		r.Get("/projects/{name}/manifest", projectHandler.GetManifest)
		r.Get("/status", projectHandler.GetStatus)

		// Checklist
		r.Get("/checklist", checklistHandler.Get)
		r.Post("/checklist/update", checklistHandler.Update)
		r.Post("/ai/agent-checklist", checklistHandler.GenerateChecklistStream)
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

		// GitHub
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

		// CLI
		r.Get("/cli/commands", cliHandler.ListAllowedCommands)
		r.Post("/cli/execute", cliHandler.ExecuteCommand)
		r.Get("/cli/cat", cliHandler.ReadFile)
		r.Get("/cli/ls", cliHandler.ListDirectory)

		// Pux Agent
		r.Route("/pux", func(r chi.Router) {
			a.puxHandler.RegisterRoutes(r)
		})

		// Sandbox
		r.Route("/sandbox", func(r chi.Router) {
			r.Post("/", a.sandboxHandler.CreateSandbox)
			r.Get("/", a.sandboxHandler.ListSandboxes)
			r.Get("/{id}", a.sandboxHandler.GetSandbox)
			r.Delete("/{id}", a.sandboxHandler.DestroySandbox)
			r.Post("/{id}/exec", a.sandboxHandler.ExecCommand)
			r.Post("/{id}/browser-mode", a.sandboxHandler.EnableBrowserMode)
			r.Post("/{id}/desktop-mode", a.sandboxHandler.EnableDesktopMode)
			r.Delete("/{id}/mode", a.sandboxHandler.DisableMode)
			r.Get("/{id}/ready", a.sandboxHandler.IsReady)
			r.Get("/{id}/vnc-health", a.sandboxHandler.VNCReadinessCheck)
			r.Get("/{id}/viewer", a.sandboxHandler.GetDesktopViewer)
			r.Route("/{id}/computer-use", func(r chi.Router) {
				a.computerUseHandler.RegisterRoutes(r)
			})
			r.Route("/{id}/x11", func(r chi.Router) {
				handlers.NewX11Handler(nil, a.logger).RegisterRoutes(r)
			})
			r.Route("/{id}/files", func(r chi.Router) {
				fileHandler.RegisterRoutes(r)
			})
			r.HandleFunc("/vnc/{id}/*", a.sandboxHandler.VNCProxy)
			r.Get("/vnc-stats", a.sandboxHandler.VNCStats)
		})

		// Artifacts
		r.Route("/pux/artifacts", func(r chi.Router) {
			artifactHandler.RegisterRoutes(r)
		})

		// Scheduler
		schedulerHandler := handlers.NewSchedulerHandler(a.sched, a.logger)
		r.Route("/scheduler", func(r chi.Router) {
			schedulerHandler.RegisterRoutes(r)
		})

		// Cluster services
		r.Route("/cluster", func(r chi.Router) {
			r.Get("/status", clusterHandler.ClusterStatus)
			r.Get("/tts", clusterHandler.TTSServices)
			r.Post("/tts/synthesize", clusterHandler.SynthesizeSpeech)
			r.Get("/asr", clusterHandler.ASRStatus)
			r.Post("/asr/transcribe", clusterHandler.TranscribeAudio)
			r.Get("/forge", clusterHandler.ForgeStatus)
			r.Post("/forge/generate", clusterHandler.ForgeGenerate)
			r.Get("/infisical", clusterHandler.InfisicalStatus)
			r.Get("/storage", clusterHandler.StorageStatus)
			r.Get("/storage/buckets", clusterHandler.StorageBuckets)
			r.Get("/storage/objects", clusterHandler.StorageListObjects)
			r.Post("/storage/upload", clusterHandler.StorageUpload)
			r.Get("/storage/download", clusterHandler.StorageDownload)
			r.Delete("/storage/delete", clusterHandler.StorageDelete)
		})
	})

	r.Handle("/*", http.FileServer(http.Dir("../dist")))
	return r
}
