package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/dashboard"
	"github.com/backlink-orchestrator/internal/database"
	"github.com/backlink-orchestrator/internal/httpapi"
	"github.com/backlink-orchestrator/internal/jobs"
	"github.com/backlink-orchestrator/internal/metrics"
	"github.com/backlink-orchestrator/internal/recovery"
	"github.com/backlink-orchestrator/internal/tasks"
	"github.com/backlink-orchestrator/internal/workers"
	"github.com/backlink-orchestrator/migrations"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Attempt to load .env file if it exists
	_ = godotenv.Load()

	cfg := config.Load()
	logger := config.SetupLogger(cfg.LogLevel)

	logger.Info("Starting Backlink Orchestrator",
		"env", cfg.AppEnv,
		"port", cfg.AppPort,
		"version", "1.0.0",
	)

	// Phase 2: Initialize Database Connection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("Failed to connect to database", "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(ctx, migrations.FS); err != nil {
		logger.Error("Failed to run migrations", "error", err.Error())
		os.Exit(1)
	}

	// Phase 3-8: Initialize Handlers and Subsystems
	workerRepo := workers.NewRepository(db)
	workerHandler := httpapi.NewWorkerHandler(workerRepo, cfg)

	taskRepo := tasks.NewRepository(db)
	taskHandler := httpapi.NewTaskHandler(taskRepo, cfg)

	jobRepo := jobs.NewRepository(db)
	adminHandler := httpapi.NewAdminHandler(jobRepo)

	recoverySvc := recovery.NewService(db, cfg)

	// Phase 10: Run recovery sequence synchronously before HTTP starts
	logger.Info("Running initial recovery scan...")
	recoverySvc.RunScanOnce(context.Background())

	go recoverySvc.Start(context.Background())

	// Start metrics updater
	metrics.StartMetricsUpdater(db)

	mux := http.NewServeMux()

	// Basic health check
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Database unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	})
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)

	// Dashboard & Auth
	dashHandler := dashboard.NewHandler(db, cfg)
	mux.HandleFunc("GET /login", dashHandler.LoginGet)
	mux.HandleFunc("POST /login", dashHandler.LoginPost)
	mux.HandleFunc("POST /logout", dashHandler.LogoutPost)

	authMux := http.NewServeMux()
	authMux.HandleFunc("GET /", dashHandler.Index)
	authMux.HandleFunc("GET /overview", dashHandler.Index)
	authMux.HandleFunc("GET /api/dashboard/stats", dashHandler.Stats)
	authMux.HandleFunc("GET /workers", dashHandler.Workers)
	authMux.HandleFunc("GET /workers/list/data", dashHandler.WorkersData)
	authMux.HandleFunc("GET /tasks", dashHandler.Tasks)
	authMux.HandleFunc("GET /tasks/list/data", dashHandler.TasksData)
	authMux.HandleFunc("GET /jobs", dashHandler.Jobs)
	authMux.HandleFunc("GET /jobs/list/data", dashHandler.JobsData)

	mux.Handle("/", metrics.Middleware(dashHandler.AuthMiddleware(authMux)))

	// API v1 (No Auth)
	mux.HandleFunc("POST /api/v1/workers/register", workerHandler.Register)

	// API v1 (Worker Auth)
	workerAuth := httpapi.WorkerAuthMiddleware(db)
	mux.Handle("POST /api/v1/workers/heartbeat", workerAuth(http.HandlerFunc(workerHandler.Heartbeat)))

	mux.Handle("POST /api/v1/tasks/claim", workerAuth(http.HandlerFunc(taskHandler.Claim)))
	mux.Handle("POST /api/v1/tasks/{task_id}/heartbeat", workerAuth(http.HandlerFunc(taskHandler.Heartbeat)))
	mux.Handle("POST /api/v1/tasks/{task_id}/complete", workerAuth(http.HandlerFunc(taskHandler.Complete)))
	mux.Handle("POST /api/v1/tasks/{task_id}/fail", workerAuth(http.HandlerFunc(taskHandler.Fail)))

	// Admin API (Uses Session Auth)
	mux.Handle("POST /api/v1/admin/jobs/{job_id}/pause", dashHandler.AuthMiddleware(http.HandlerFunc(adminHandler.PauseJob)))
	mux.Handle("POST /api/v1/admin/jobs/{job_id}/resume", dashHandler.AuthMiddleware(http.HandlerFunc(adminHandler.ResumeJob)))
	mux.Handle("POST /api/v1/admin/jobs/{job_id}/cancel", dashHandler.AuthMiddleware(http.HandlerFunc(adminHandler.CancelJob)))

	serverAddr := ":" + cfg.AppPort
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	go func() {
		logger.Info("Listening for requests", "addr", serverAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Server failed", "error", err.Error())
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server forced to shutdown", "error", err.Error())
	}

	logger.Info("Server exited properly")
}
