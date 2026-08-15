package integration

import (
	"context"
	"testing"
	"time"

	"github.com/backlink-orchestrator/internal/jobs"
	"github.com/backlink-orchestrator/internal/recovery"
	"github.com/backlink-orchestrator/internal/tasks"
	"github.com/backlink-orchestrator/internal/workers"
)

func TestRecoveryLoop(t *testing.T) {
	db, cfg := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	taskRepo := tasks.NewRepository(db)
	jobRepo := jobs.NewRepository(db)
	workerRepo := workers.NewRepository(db)
	recoverySvc := recovery.NewService(db, cfg)

	// Register a worker
	w1 := workers.Worker{
		WorkerID:     "dead-worker-1",
		Hostname:     "host",
		OS:           "linux",
		Architecture: "amd64",
		CPUCount:     1,
		MemoryMB:     512,
		Version:      "1.0",
		TokenHash:    "token",
	}
	workerRepo.RegisterWorker(ctx, w1)

	// Create job
	jobReq := jobs.JobCreate{
		Dataset:     "TEST",
		CrawlID:     "1",
		SourcePaths: []string{"path1"},
	}
	jobRepo.CreateJob(ctx, jobReq)

	// Claim task
	res, _ := taskRepo.ClaimTask(ctx, "dead-worker-1", 1*time.Millisecond) // VERY short lease

	// Simulate worker death (manipulate DB directly to simulate timeout)
	db.ExecContext(ctx, "UPDATE workers SET last_heartbeat_at = NOW() - INTERVAL '100 seconds' WHERE worker_id = 'dead-worker-1'")
	db.ExecContext(ctx, "UPDATE tasks SET lease_until = NOW() - INTERVAL '10 seconds' WHERE task_id = $1", res.TaskID)

	// Run recovery scan
	recoverySvc.RunScanOnce(ctx)

	// Assert worker is offline
	var workerStatus string
	db.QueryRowContext(ctx, "SELECT status FROM workers WHERE worker_id = 'dead-worker-1'").Scan(&workerStatus)
	if workerStatus != "OFFLINE" {
		t.Errorf("expected worker to be OFFLINE, got %s", workerStatus)
	}

	// Assert task is queued again
	var taskStatus string
	db.QueryRowContext(ctx, "SELECT status FROM tasks WHERE task_id = $1", res.TaskID).Scan(&taskStatus)
	if taskStatus != "QUEUED" {
		t.Errorf("expected task to be recovered to QUEUED, got %s", taskStatus)
	}

	// Another worker claims the task
	w2 := workers.Worker{
		WorkerID:     "live-worker",
		Hostname:     "host",
		OS:           "linux",
		Architecture: "amd64",
		CPUCount:     1,
		MemoryMB:     512,
		Version:      "1.0",
		TokenHash:    "token2",
	}
	workerRepo.RegisterWorker(ctx, w2)

	res2, err := taskRepo.ClaimTask(ctx, "live-worker", 10*time.Minute)
	if err != nil {
		t.Fatalf("expected to claim recovered task, got err: %v", err)
	}

	if res2.TaskID != res.TaskID {
		t.Errorf("expected to claim the same task ID")
	}
}
