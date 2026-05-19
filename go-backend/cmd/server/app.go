package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal"
	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/autoconfig"
	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"github.com/auto-developer-orchestrator/backend/internal/engines"
	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/extensions"
	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/manifest"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/models"
	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"github.com/auto-developer-orchestrator/backend/internal/observability"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/scheduler"
	schedulertool "github.com/auto-developer-orchestrator/backend/internal/tools/scheduler"
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
	x11Handler         *handlers.X11Handler
	sched              *scheduler.Scheduler
	promPusher         *observability.MetricsPusher
	extMgr             *extensions.Manager
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
	if a.puxHandler != nil {
		a.puxHandler.CloseSSH()
	}
	if a.sched != nil {
		a.sched.Stop()
	}
	if a.extMgr != nil {
		a.extMgr.StopAll()
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

	// SSH session manager for remote filesystem browsing
	sshManager := puxssh.NewSessionManager(logger)
	a.puxHandler.SetSSHManager(sshManager)

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
	a.sandboxHandler = handlers.NewSandboxHandler(sandboxMgr, logger, a.db)

	// Vision
	visionURL := os.Getenv("LITELLM_PROXY_URL")
	visionKey := os.Getenv("LITELLM_MASTER_KEY")
	visionClient := browser.NewVisionClient(visionURL, visionKey, modelCfg)

	// Computer Use + X11
	a.computerUseHandler = handlers.NewComputerUseHandler(sandboxMgr, visionClient, logger)
	a.x11Handler = handlers.NewX11Handler(sandboxMgr, logger)
	x11Handler := a.x11Handler

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

	webResearchClient := mcp.NewClient("web", hubBase+"/mcp/web", a.logger)
	mcpMulti.AddClient("web", webResearchClient)

	mediaAnalysisClient := mcp.NewClient("media", hubBase+"/mcp/media", a.logger)
	mcpMulti.AddClient("media", mediaAnalysisClient)

	// Start extension subprocesses
	a.extMgr = extensions.NewManager(a.logger)
	projectRoot := os.Getenv("PROJECT_ROOT")
	extDirs := []string{
		filepath.Join(projectRoot, "extensions"),
		filepath.Join(os.Getenv("HOME"), ".pux", "extensions"),
	}
	// Discover org-scoped extensions from all known organizations
	orgExtDirs := discoverOrgExtensionDirs()
	if len(orgExtDirs) > 0 {
		extDirs = append(extDirs, orgExtDirs...)
		a.logger.Info("Org extension directories discovered", zap.Int("count", len(orgExtDirs)))
	}
	started := a.extMgr.StartAll(context.Background(), extDirs...)
	if started > 0 {
		for prefix, client := range a.extMgr.Clients() {
			mcpMulti.AddClient(prefix, client)
		}
		a.logger.Info("Extension servers started", zap.Int("count", started))
	}

	// Load user-configured MCP servers from settings.json
	if homeDir, err := os.UserHomeDir(); err == nil {
		settingsPath := filepath.Join(homeDir, ".pi", "agent", "settings.json")
		if data, err := os.ReadFile(settingsPath); err == nil {
			var cfg struct {
				MCPServers map[string]string `json:"mcpServers"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				for prefix, endpoint := range cfg.MCPServers {
					if !mcpMulti.HasClient(prefix) {
						mcpMulti.AddClient(prefix, mcp.NewClient(prefix, endpoint, a.logger))
						a.logger.Info("Loaded persisted MCP server", zap.String("prefix", prefix), zap.String("endpoint", endpoint))
					}
				}
			}
		}
	}

	if mcpMulti.IsAvailable() {
		// Wire MCP instruction registration → prompt builder (avoids circular import)
		mcp.InstructionRegistrar = common.RegisterMCPInstructions
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

	// Create PromptSender that calls the local /api/pux/prompt endpoint
	projectRoot := os.Getenv("PROJECT_ROOT")
	promptSender := makeLocalPromptSender("http://localhost:3847", projectRoot, a.logger)

	a.sched = scheduler.NewScheduler(storePath, promptSender, a.logger)

	runLogMgr, err := scheduler.NewRunLogManager("")
	if err != nil {
		a.logger.Warn("Failed to create run log manager", zap.Error(err))
	}
	a.sched.SetRunLogManager(runLogMgr, projectRoot)

	if err := a.sched.Start(context.Background()); err != nil {
		a.logger.Warn("Failed to start scheduler", zap.Error(err))
	}

	// Wire scheduler backend for LLM tool access (adapter avoids import cycle)
	if a.puxHandler != nil {
		a.puxHandler.SetSchedulerTool(&schedulerBackend{inner: a.sched})
	}
}

// schedulerBackend adapts *scheduler.Scheduler to schedulertool.Backend.
type schedulerBackend struct {
	inner *scheduler.Scheduler
}

// Compile-time check that schedulerBackend implements schedulertool.Backend.
var _ schedulertool.Backend = (*schedulerBackend)(nil)

func (b *schedulerBackend) ListJobsInfo() []*schedulertool.JobInfo {
	jobs := b.inner.ListJobs()
	result := make([]*schedulertool.JobInfo, len(jobs))
	for i, j := range jobs {
		result[i] = &schedulertool.JobInfo{
			ID:               j.ID,
			Name:             j.Name,
			Description:      j.Description,
			Project:          j.Project,
			Message:          j.Message,
			Model:            j.Model,
			Schedule:         string(j.Schedule),
			CronExpr:         j.CronExpr,
			EverySeconds:     j.EverySeconds,
			AtTime:           j.AtTime,
			Enabled:          j.Enabled,
			Status:           string(j.Status),
			LastRunAt:        j.LastRunAt,
			LastRunStatus:    j.LastRunStatus,
			LastError:        j.LastError,
			NextRunAt:        j.NextRunAt,
			ConsecutiveErrors: j.ConsecutiveErrors,
			InputTokens:      j.InputTokens,
			OutputTokens:     j.OutputTokens,
			DurationMs:       j.DurationMs,
		}
	}
	return result
}

func (b *schedulerBackend) FindJobByNameOrID(nameOrID string) *schedulertool.JobInfo {
	jobs := b.inner.ListJobs()
	for _, j := range jobs {
		if j.Name == nameOrID || j.ID == nameOrID {
			return &schedulertool.JobInfo{
				ID: j.ID, Name: j.Name, Description: j.Description,
				Project: j.Project, Message: j.Message, Model: j.Model,
				Schedule: string(j.Schedule), CronExpr: j.CronExpr,
				EverySeconds: j.EverySeconds, AtTime: j.AtTime,
				Enabled: j.Enabled, Status: string(j.Status),
				LastRunAt: j.LastRunAt, LastRunStatus: j.LastRunStatus,
				LastError: j.LastError, NextRunAt: j.NextRunAt,
				ConsecutiveErrors: j.ConsecutiveErrors,
				InputTokens: j.InputTokens, OutputTokens: j.OutputTokens,
				DurationMs: j.DurationMs,
			}
		}
	}
	return nil
}

func (b *schedulerBackend) CreateJobParams(name, project, message, scheduleType, cronExpr, atTime, description, model string, everySeconds int64, enabled bool) (string, error) {
	job := &scheduler.Job{
		Name: name, Project: project, Message: message,
		Schedule: scheduler.ScheduleType(scheduleType),
		CronExpr: cronExpr, AtTime: atTime, Description: description,
		Model: model, EverySeconds: everySeconds, Enabled: enabled,
	}
	if err := b.inner.CreateJob(job); err != nil {
		return "", err
	}
	return job.ID, nil
}

func (b *schedulerBackend) UpdateJobParams(id, name, message, project, model, description, scheduleType, cronExpr, atTime string, everySeconds int64, enabled *bool) error {
	updates := &scheduler.Job{}
	if name != "" {
		updates.Name = name
	}
	if message != "" {
		updates.Message = message
	}
	if project != "" {
		updates.Project = project
	}
	if model != "" {
		updates.Model = model
	}
	if description != "" {
		updates.Description = description
	}
	if scheduleType != "" {
		updates.Schedule = scheduler.ScheduleType(scheduleType)
	}
	if cronExpr != "" {
		updates.CronExpr = cronExpr
	}
	if atTime != "" {
		updates.AtTime = atTime
	}
	if everySeconds > 0 {
		updates.EverySeconds = everySeconds
	}
	if enabled != nil {
		updates.Enabled = *enabled
	}
	return b.inner.UpdateJob(id, updates)
}

func (b *schedulerBackend) DeleteJob(id string) error             { return b.inner.DeleteJob(id) }
func (b *schedulerBackend) TriggerJob(id string) error            { return b.inner.TriggerJob(id) }

func (b *schedulerBackend) ListRunsInfo(jobID string, limit int) []schedulertool.RunInfo {
	entries, err := b.inner.ListRuns(jobID, limit, "")
	if err != nil {
		return nil
	}
	result := make([]schedulertool.RunInfo, len(entries))
	for i, e := range entries {
		result[i] = schedulertool.RunInfo{
			Ts: e.Ts, Status: e.Status, Summary: e.Summary,
			Error: e.Error, JobName: e.JobName,
		}
	}
	return result
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
			status["version"] = internal.Version
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
		})

		// Projects
		r.Get("/projects", projectHandler.List)
		r.Post("/projects/add", projectHandler.Add)
		r.Post("/projects/register", projectHandler.Add)
		r.Delete("/projects", projectHandler.Remove)
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

		// Filesystem browser (for "Open Folder" dialog)
		fsBrowseHandler := handlers.NewFsBrowseHandler()
		r.Get("/fs/browse", fsBrowseHandler.Browse)

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
				a.x11Handler.RegisterRoutes(r)
			})
			r.Route("/{id}/files", func(r chi.Router) {
				fileHandler.RegisterRoutes(r)
			})
			r.HandleFunc("/vnc/{id}/*", a.sandboxHandler.VNCProxy)
			r.Get("/vnc-stats", a.sandboxHandler.VNCStats)
		})

		// Terminal WebSocket
		r.Get("/terminal/ws", a.sandboxHandler.TerminalWS)

		// Artifacts
		r.Route("/pux/artifacts", func(r chi.Router) {
			artifactHandler.RegisterRoutes(r)
		})

		// Scheduler
		schedulerHandler := handlers.NewSchedulerHandler(a.sched, a.logger)
		r.Route("/scheduler", func(r chi.Router) {
			schedulerHandler.RegisterRoutes(r)
		})

		// Workers
		workersDir := filepath.Join(common.FindKernelConfigDir(), "workers")
		workersStore := autoconfig.NewWorkerStore(workersDir)
		workerHandler := handlers.NewWorkerHandler(workersStore, a.logger)
		r.Route("/workers", func(r chi.Router) {
			workerHandler.RegisterRoutes(r)
		})

		// CTO prompt sections — editable prompt template files
		configDir := common.FindKernelConfigDir()
		promptHandler := handlers.NewPromptSectionHandler(configDir, a.logger)
		r.Route("/prompt-sections", func(r chi.Router) {
			promptHandler.RegisterRoutes(r)
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

	r.Handle("/*", http.FileServer(http.Dir("../dist-web")))
	return r
}

// makeLocalPromptSender creates a PromptSender that calls the local /api/pux/prompt endpoint.
// The scheduler uses this to execute jobs — same path as orch agent prompt, web UI, and TUI.
func makeLocalPromptSender(baseURL, projectRoot string, logger *zap.Logger) scheduler.PromptSender {
	return func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge, sandboxOnly bool) (string, error) {
		// If org is set, resolve it to a project path (same as CLI --org)
		effectiveProject := project
		if org != "" {
			if resolved, err := resolveOrgPathLocal(org); err == nil {
				effectiveProject = resolved
			} else {
				logger.Warn("failed to resolve org, using project as-is", zap.String("org", org), zap.Error(err))
			}
		}

		payload := map[string]interface{}{
			"message":     message,
			"project":     effectiveProject,
			"autoBranch":  autoBranch,
			"autoMerge":   autoMerge,
			"sandboxOnly": sandboxOnly,
		}
		if agentID != "" {
			payload["agentId"] = agentID
		}
		if model != "" {
			payload["model"] = model
		}

		data, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("marshal prompt request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pux/prompt", bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("create prompt request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("send prompt request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errBody struct {
				Error string `json:"error"`
			}
			if jsonErr := json.NewDecoder(resp.Body).Decode(&errBody); jsonErr == nil && errBody.Error != "" {
				return "", fmt.Errorf("prompt returned %d: %s", resp.StatusCode, errBody.Error)
			}
			return "", fmt.Errorf("prompt returned %d", resp.StatusCode)
		}

		// Parse SSE stream — collect text_delta events into combined output
		var output strings.Builder
		scanner := bufio.NewScanner(resp.Body)
		var currentEvent string

		for scanner.Scan() {
			line := scanner.Text()

			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				continue
			}

			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")

				// Stream end
				if dataStr == "[DONE]" {
					break
				}

				// Collect text deltas
				if currentEvent == "text_delta" {
					var evt struct {
						Text string `json:"text"`
					}
					if json.Unmarshal([]byte(dataStr), &evt) == nil && evt.Text != "" {
						output.WriteString(evt.Text)
					}
				}

				// Check for error events
				if currentEvent == "error" {
					var errEvt struct {
						Error string `json:"error"`
					}
					if json.Unmarshal([]byte(dataStr), &errEvt) == nil && errEvt.Error != "" {
						return output.String(), fmt.Errorf("agent error: %s", errEvt.Error)
					}
				}
			}
		}

		return output.String(), scanner.Err()
	}
}

// resolveOrgPathLocal resolves an org name to its directory path.
// Mirrors the CLI's resolveOrgPath logic — searches standard locations for pux.yaml.
func resolveOrgPathLocal(name string) (string, error) {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Documents", "programs", "dev", name),
		filepath.Join(home, "Documents", "programs", "dev", name+"-org"),
		filepath.Join(home, "Documents", "projects", name, "pux-org"),
		filepath.Join(home, "Documents", "projects", name),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "pux.yaml")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("organization '%s' not found", name)
}

// discoverOrgExtensionDirs scans known org locations for directories containing
// pux.yaml with an extensions_dir configured. Returns list of extension directories.
func discoverOrgExtensionDirs() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	// Scan standard org parent directories for pux.yaml
	parentDirs := []string{
		filepath.Join(home, "Documents", "programs", "dev"),
		filepath.Join(home, "Documents", "projects"),
	}

	var extDirs []string
	for _, parent := range parentDirs {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			orgDir := filepath.Join(parent, entry.Name())

			// Check for pux.yaml directly
			org := common.LoadOrgManifest(orgDir)
			if org == nil {
				// Also check pux-org subdirectory
				orgDir2 := filepath.Join(orgDir, "pux-org")
				org = common.LoadOrgManifest(orgDir2)
				if org == nil {
					continue
				}
			}

			if extDir := org.ExtensionsDirPath(); extDir != "" {
				if _, err := os.Stat(extDir); err == nil {
					extDirs = append(extDirs, extDir)
				}
			}
		}
	}
	return extDirs
}
