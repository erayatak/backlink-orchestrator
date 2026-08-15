package recovery

import (
	"context"
	"log/slog"
	"time"

	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/database"
)

type Service struct {
	db  *database.DB
	cfg *config.Config
}

func NewService(db *database.DB, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg}
}

// RunScanOnce executes a single recovery scan synchronously.
func (s *Service) RunScanOnce(ctx context.Context) {
	slog.Info("Starting single recovery scan")
	s.markOfflineWorkers(ctx)
	s.recoverExpiredTasks(ctx)
	s.updateJobProgress(ctx)
	slog.Info("Finished single recovery scan")
}

// Start begins the recovery loop which runs every 10 seconds.
func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping recovery loop")
			return
		case <-ticker.C:
			s.runRecoveryPass(ctx)
		}
	}
}

func (s *Service) runRecoveryPass(ctx context.Context) {
	// 1. Mark workers OFFLINE if last_heartbeat_at <= now - timeout
	s.markOfflineWorkers(ctx)

	// 2. Task recovery: Re-queue tasks with expired leases
	s.recoverExpiredTasks(ctx)

	// 3. Update job progress metrics
	s.updateJobProgress(ctx)
}

func (s *Service) markOfflineWorkers(ctx context.Context) {
	timeout := s.cfg.HeartbeatTimeout

	res, err := s.db.ExecContext(ctx, `
		UPDATE workers
		SET status = 'OFFLINE', updated_at = NOW()
		WHERE status != 'OFFLINE' AND status != 'DISABLED'
		  AND last_heartbeat_at <= NOW() - $1::interval
	`, timeout.String())

	if err != nil {
		slog.Error("Failed to mark offline workers", "error", err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected > 0 {
		slog.Info("Marked workers OFFLINE due to heartbeat timeout", "count", affected)
	}
}

func (s *Service) recoverExpiredTasks(ctx context.Context) {
	// Transition expired tasks to QUEUED if they are still retryable, else FAILED.
	// Since we don't know the error code, we will log it as SYSTEM_RECOVERY.

	// Phase 1: Re-queue tasks that have not reached MaxAttempts
	res, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'QUEUED', assigned_worker_id = NULL, lease_until = NULL, updated_at = NOW()
		WHERE status = 'LEASED' AND lease_until <= NOW() AND current_attempt < $1
	`, s.cfg.TaskMaxAttempts)

	if err != nil {
		slog.Error("Failed to requeue expired tasks", "error", err.Error())
		return
	}

	affectedQueued, _ := res.RowsAffected()
	if affectedQueued > 0 {
		slog.Info("Re-queued expired tasks", "count", affectedQueued)
	}

	// Phase 2: Fail tasks that have reached MaxAttempts
	resFail, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'FAILED', assigned_worker_id = NULL, lease_until = NULL, updated_at = NOW()
		WHERE status = 'LEASED' AND lease_until <= NOW() AND current_attempt >= $1
	`, s.cfg.TaskMaxAttempts)

	if err != nil {
		slog.Error("Failed to fail expired tasks", "error", err.Error())
		return
	}

	affectedFailed, _ := resFail.RowsAffected()
	if affectedFailed > 0 {
		slog.Info("Failed expired tasks permanently (max attempts reached)", "count", affectedFailed)
	}
}

func (s *Service) updateJobProgress(ctx context.Context) {
	// This query recalculates task counts for all active jobs
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs j
		SET 
			queued_tasks = (SELECT count(*) FROM tasks WHERE job_id = j.id AND status = 'QUEUED'),
			running_tasks = (SELECT count(*) FROM tasks WHERE job_id = j.id AND status IN ('LEASED', 'RUNNING')),
			succeeded_tasks = (SELECT count(*) FROM tasks WHERE job_id = j.id AND status = 'SUCCEEDED'),
			failed_tasks = (SELECT count(*) FROM tasks WHERE job_id = j.id AND status = 'FAILED'),
			updated_at = NOW()
		WHERE status NOT IN ('COMPLETED', 'CANCELLED');
	`)

	if err != nil {
		slog.Error("Failed to update job progress metrics", "error", err.Error())
		return
	}

	// Transition jobs that are finished
	_, _ = s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = CASE 
				WHEN succeeded_tasks = total_tasks THEN 'COMPLETED'
				WHEN (succeeded_tasks + failed_tasks) = total_tasks THEN 'FAILED'
				ELSE status
			END,
			finished_at = CASE
				WHEN (succeeded_tasks + failed_tasks) = total_tasks THEN NOW()
				ELSE NULL
			END
		WHERE status NOT IN ('COMPLETED', 'CANCELLED')
		  AND (succeeded_tasks + failed_tasks) = total_tasks;
	`)
}
