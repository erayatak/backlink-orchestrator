package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/backlink-orchestrator/internal/auth"
	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/database"
	"github.com/backlink-orchestrator/internal/httpapi"
	"github.com/backlink-orchestrator/internal/jobs"
	"github.com/backlink-orchestrator/internal/tasks"
	"github.com/backlink-orchestrator/internal/workers"
	"github.com/backlink-orchestrator/migrations"
)

func setupTestServer(t *testing.T) (*httptest.Server, *database.DB) {
	cfg := config.Load()
	db, _ := database.Connect(context.Background(), cfg.DatabaseURL)
	_, _ = db.Exec("TRUNCATE workers, jobs, tasks, task_attempts, worker_heartbeat_history, system_events, admin_sessions, bootstrap_tokens CASCADE")
	db.Migrate(context.Background(), migrations.FS)

	// Create a bootstrap token for the test
	hashedToken := auth.HashToken("test-token-hash")
	db.Exec("INSERT INTO bootstrap_tokens (token_hash, expires_at, created_by, status) VALUES ($1, NOW() + INTERVAL '1 hour', 'test', 'ACTIVE')", hashedToken)

	workerRepo := workers.NewRepository(db)
	workerHandler := httpapi.NewWorkerHandler(workerRepo, cfg)

	taskRepo := tasks.NewRepository(db)
	taskHandler := httpapi.NewTaskHandler(taskRepo, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/workers/register", workerHandler.Register)

	workerAuth := httpapi.WorkerAuthMiddleware(db)
	mux.Handle("POST /api/v1/workers/heartbeat", workerAuth(http.HandlerFunc(workerHandler.Heartbeat)))
	mux.Handle("POST /api/v1/tasks/claim", workerAuth(http.HandlerFunc(taskHandler.Claim)))
	mux.Handle("POST /api/v1/tasks/{task_id}/complete", workerAuth(http.HandlerFunc(taskHandler.Complete)))

	srv := httptest.NewServer(mux)
	return srv, db
}

func TestE2E_FullLifecycle(t *testing.T) {
	srv, db := setupTestServer(t)
	defer srv.Close()
	defer db.Close()

	// 1. Create Job manually in DB
	jobRepo := jobs.NewRepository(db)
	jobReq := jobs.JobCreate{Dataset: "E2E", CrawlID: "1", SourcePaths: []string{"file-1"}}
	jobRepo.CreateJob(context.Background(), jobReq)

	// 2. Register Worker
	reqBody := `{"worker_id":"w-e2e","hostname":"test","os":"linux","architecture":"amd64","cpu_count":2,"memory_mb":1024,"version":"1.0"}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/workers/register", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer test-token-hash")

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			buf := new(bytes.Buffer)
			buf.ReadFrom(resp.Body)
			t.Fatalf("failed to register worker: %v, status: %v, body: %s", err, resp.StatusCode, buf.String())
		}
		t.Fatalf("failed to register worker: %v", err)
	}

	var regRes struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&regRes)
	tokenRaw := regRes.Token

	// 3. Claim Task
	reqClaimBody := `{"worker_id":"w-e2e","capacity":{"cpu_count":2,"memory_mb":1024}}`
	reqClaim, _ := http.NewRequest("POST", srv.URL+"/api/v1/tasks/claim", bytes.NewBufferString(reqClaimBody))
	reqClaim.Header.Set("Authorization", "Bearer "+tokenRaw)

	respClaim, err := http.DefaultClient.Do(reqClaim)
	if err != nil || respClaim.StatusCode != http.StatusOK {
		if respClaim != nil {
			buf := new(bytes.Buffer)
			buf.ReadFrom(respClaim.Body)
			t.Fatalf("failed to claim task: %v, status: %v, body: %s", err, respClaim.StatusCode, buf.String())
		}
		t.Fatalf("failed to claim task: %v", err)
	}
	var claimRes struct {
		TaskID string `json:"task_id"`
	}
	json.NewDecoder(respClaim.Body).Decode(&claimRes)

	// 4. Complete Task
	reqCompleteBody := `{"attempt_id":"1","processed_bytes":100,"records_processed":10,"links_found":5,"output_uri":"r2://out"}`
	reqComplete, _ := http.NewRequest("POST", srv.URL+"/api/v1/tasks/"+claimRes.TaskID+"/complete", bytes.NewBufferString(reqCompleteBody))
	reqComplete.Header.Set("Authorization", "Bearer "+tokenRaw)

	respComplete, err := http.DefaultClient.Do(reqComplete)
	if err != nil || respComplete.StatusCode != http.StatusOK {
		t.Fatalf("failed to complete task")
	}

	// Verify Job is complete
	var internalJobID string
	db.QueryRow("SELECT id FROM jobs WHERE dataset = 'E2E'").Scan(&internalJobID)
	jobRepo.UpdateJobProgress(context.Background(), internalJobID)

	var jobStatus string
	db.QueryRow("SELECT status FROM jobs WHERE dataset = 'E2E'").Scan(&jobStatus)
	if jobStatus != "COMPLETED" {
		t.Errorf("expected job to be COMPLETED, got %s", jobStatus)
	}

	// 5. Test Job Pause/Resume
	jobRepo.UpdateJobStatus(context.Background(), "E2E-1", "PAUSED")
	db.QueryRow("SELECT status FROM jobs WHERE dataset = 'E2E'").Scan(&jobStatus)
	if jobStatus != "PAUSED" {
		t.Errorf("expected job PAUSED, got %s", jobStatus)
	}
}
