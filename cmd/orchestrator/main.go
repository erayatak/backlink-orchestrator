package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/backlink-orchestrator/internal/auth"
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
	_ = godotenv.Load()

	if len(os.Args) < 2 {
		runServer()
		return
	}

	switch os.Args[1] {
	case "server":
		runServer()
	case "migrate":
		runMigrate()
	case "admin":
		if len(os.Args) < 3 {
			fmt.Println("Usage: orchestrator admin [password-hash | bootstrap-token create]")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "password-hash":
			if len(os.Args) < 4 {
				fmt.Println("Usage: orchestrator admin password-hash <password>")
				os.Exit(1)
			}
			hash, err := auth.GenerateArgon2idHash(os.Args[3])
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(hash)
		case "bootstrap-token":
			if len(os.Args) < 4 || os.Args[3] != "create" {
				fmt.Println("Usage: orchestrator admin bootstrap-token create")
				os.Exit(1)
			}
			createBootstrapToken()
		default:
			fmt.Println("Unknown admin command:", os.Args[2])
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("Available commands: server, migrate, admin")
		os.Exit(1)
	}
}

func runMigrate() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(ctx, migrations.FS); err != nil {
		fmt.Printf("Failed to run migrations: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Migrations applied successfully.")
}

func createBootstrapToken() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	token, err := auth.GenerateToken(32)
	if err != nil {
		fmt.Printf("Failed to generate token: %v\n", err)
		os.Exit(1)
	}

	tokenHash := auth.HashToken(token)
	expiresAt := time.Now().Add(24 * time.Hour * 365) // Default 1 year for bootstrap tokens
	createdBy := "cli_admin"

	_, err = db.ExecContext(ctx, `
		INSERT INTO bootstrap_tokens (token_hash, expires_at, created_by, status)
		VALUES ($1, $2, $3, 'ACTIVE')
	`, tokenHash, expiresAt, createdBy)

	if err != nil {
		fmt.Printf("Failed to insert token: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Bootstrap token generated successfully!")
	fmt.Printf("Token (Plaintext, KEEP THIS SECRET): %s\n", token)
	fmt.Printf("Expires At: %v\n", expiresAt.Format(time.RFC3339))
	fmt.Printf("Created By: %s\n", createdBy)
}

func runServer() {
	cfg := config.Load()
	logger := config.SetupLogger(cfg.LogLevel)

	logger.Info("Starting Backlink Orchestrator",
		"env", cfg.AppEnv,
		"port", cfg.AppPort,
		"version", "1.0.0",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("Failed to connect to database", "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	// Optionally keep migrate here as well for automatic migration, or remove it so users MUST use CLI.
	// We'll keep it for local dev convenience, but in prod install script we will call `orchestrator migrate` first anyway.
	if err := db.Migrate(ctx, migrations.FS); err != nil {
		logger.Error("Failed to run migrations", "error", err.Error())
		os.Exit(1)
	}

	workerRepo := workers.NewRepository(db)
	workerHandler := httpapi.NewWorkerHandler(workerRepo, cfg)

	taskRepo := tasks.NewRepository(db)
	taskHandler := httpapi.NewTaskHandler(taskRepo, cfg)

	jobRepo := jobs.NewRepository(db)
	adminHandler := httpapi.NewAdminHandler(jobRepo)

	recoverySvc := recovery.NewService(db, cfg)

	logger.Info("Running initial recovery scan...")
	recoverySvc.RunScanOnce(context.Background())

	go recoverySvc.Start(context.Background())

	metrics.StartMetricsUpdater(db)

	mux := http.NewServeMux()

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

	mux.HandleFunc("POST /api/v1/workers/register", workerHandler.Register)

	workerAuth := httpapi.WorkerAuthMiddleware(db)
	mux.Handle("POST /api/v1/workers/heartbeat", workerAuth(http.HandlerFunc(workerHandler.Heartbeat)))
	mux.Handle("POST /api/v1/tasks/claim", workerAuth(http.HandlerFunc(taskHandler.Claim)))
	mux.Handle("POST /api/v1/tasks/{task_id}/heartbeat", workerAuth(http.HandlerFunc(taskHandler.Heartbeat)))
	mux.Handle("POST /api/v1/tasks/{task_id}/complete", workerAuth(http.HandlerFunc(taskHandler.Complete)))
	mux.Handle("POST /api/v1/tasks/{task_id}/fail", workerAuth(http.HandlerFunc(taskHandler.Fail)))

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
