package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/database"
	"github.com/backlink-orchestrator/internal/jobs"
	"github.com/backlink-orchestrator/internal/tasks"
	"github.com/backlink-orchestrator/internal/workers"
	"github.com/backlink-orchestrator/migrations"
)

func setupTestDB(t *testing.T) (*database.DB, *config.Config) {
	cfg := config.Load() // Must have a test database configured
	ctx := context.Background()

	// Use the normal DB but we could wipe it for test
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("failed to connect db: %v", err)
	}

	// Wipe all
	_, _ = db.ExecContext(ctx, "TRUNCATE workers, jobs, tasks, task_attempts, worker_heartbeat_history, system_events, admin_sessions, bootstrap_tokens CASCADE")

	if err := db.Migrate(ctx, migrations.FS); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	return db, cfg
}

func TestConcurrentTaskClaim(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	taskRepo := tasks.NewRepository(db)
	jobRepo := jobs.NewRepository(db)
	workerRepo := workers.NewRepository(db)

	// Register 100 workers
	workerIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		workerID := fmt.Sprintf("worker_%d", i)
		workerIDs[i] = workerID
		w := workers.Worker{
			WorkerID:     workerID,
			Hostname:     "test-host",
			OS:           "linux",
			Architecture: "amd64",
			CPUCount:     2,
			MemoryMB:     1024,
			Version:      "1.0.0",
			TokenHash:    "test-token",
		}
		err := workerRepo.RegisterWorker(ctx, w)
		if err != nil {
			t.Fatalf("failed to register worker: %v", err)
		}
	}

	// Create 1 job with 1000 tasks
	sourcePaths := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		sourcePaths[i] = fmt.Sprintf("path/to/file-%d", i)
	}
	jobReq := jobs.JobCreate{
		Dataset:     "TEST-100",
		CrawlID:     "CRAWL-1",
		SourcePaths: sourcePaths,
	}
	err := jobRepo.CreateJob(ctx, jobReq)
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	var wg sync.WaitGroup
	var successCount int
	var mu sync.Mutex

	// 100 concurrent workers trying to claim tasks
	start := time.Now()
	for _, wID := range workerIDs {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			for {
				res, err := taskRepo.ClaimTask(ctx, workerID, 10*time.Minute)
				if err != nil {
					if err == tasks.ErrNoTasksAvailable {
						break
					}
					t.Errorf("unexpected error on claim: %v", err)
					break
				}

				// Simulate tiny amount of work
				time.Sleep(2 * time.Millisecond)

				completeData := tasks.CompleteData{
					TaskID:           res.TaskID,
					WorkerID:         workerID,
					ProcessedBytes:   100,
					ProcessedRecords: 10,
					LinksFound:       5,
					BacklinksFound:   1,
					OutputURI:        "r2://test",
				}
				err = taskRepo.CompleteTask(ctx, completeData)
				if err != nil {
					t.Errorf("failed to complete task: %v", err)
				} else {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}
		}(wID)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("Completed %d tasks in %v with 100 concurrent workers", successCount, duration)

	if successCount != 1000 {
		t.Errorf("expected 1000 successful completions, got %d", successCount)
	}

	// Verify DB state
	var queued, running, succeeded int
	db.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE status = 'QUEUED'").Scan(&queued)
	db.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE status IN ('LEASED', 'RUNNING')").Scan(&running)
	db.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE status = 'SUCCEEDED'").Scan(&succeeded)

	if queued != 0 || running != 0 || succeeded != 1000 {
		t.Errorf("DB state mismatch: queued=%d running=%d succeeded=%d", queued, running, succeeded)
	}
}
