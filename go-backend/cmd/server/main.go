package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/handlers"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Initialize database
	db, err := storage.NewDatabase(":memory:") // TODO: Load from env
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer db.Close()

	// Initialize handlers
	checklistHandler := handlers.NewChecklistHandler(db, logger)
	projectHandler := handlers.NewProjectHandler(db, logger)
	julesHandler := handlers.NewJulesHandler(db, logger)
	aiHandler := handlers.NewAIHandler(logger) // Will call Python microservice
	configHandler := handlers.NewConfigHandler(logger)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
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
		r.Post("/clone", projectHandler.Clone)

		// Status
		r.Get("/status", projectHandler.GetStatus)

		// Checklist
		r.Get("/checklist", checklistHandler.Get)
		r.Post("/checklist/update", checklistHandler.Update)
		r.Post("/ai/agent-checklist", aiHandler.GenerateChecklist) // SSE streaming

		// Task Dispatch
		r.Post("/dispatch", julesHandler.Dispatch)
		r.Post("/dispatch/all", julesHandler.DispatchAll)

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

		// Settings
		r.Post("/settings/mode", projectHandler.SetMode)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}
