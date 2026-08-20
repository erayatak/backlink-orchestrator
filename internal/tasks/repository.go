package tasks

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/backlink-orchestrator/internal/database"
)

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

type Task struct {
	ID               string
	TaskID           string
	JobID            string
	Dataset          string
	SourcePath       string
	Status           string
	AssignedWorkerID *string
	CurrentAttempt   int
	LeaseUntil       *time.Time
	CrawlID          string
	PipelineVersion  string
}

type ClaimResult struct {
	TaskID          string
	Dataset         string
	SourcePath      string
	CrawlID         string
	PipelineVersion string
	LeaseUntil      time.Time
}

var ErrNoTasksAvailable = errors.New("no tasks available")

// ClaimTask claims an available task for a worker.
func (r *Repository) ClaimTask(ctx context.Context, workerID string, leaseDuration time.Duration) (*ClaimResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Find an available task using SKIP LOCKED
	var task Task
	err = tx.QueryRowContext(ctx, `
		SELECT t.id, t.task_id, t.job_id, t.dataset, t.source_path, t.current_attempt, j.crawl_id, j.pipeline_version
		FROM tasks t
		JOIN jobs j ON t.job_id = j.id
		WHERE t.status = 'QUEUED' 
		ORDER BY t.created_at ASC 
		FOR UPDATE SKIP LOCKED 
		LIMIT 1
	`).Scan(&task.ID, &task.TaskID, &task.JobID, &task.Dataset, &task.SourcePath, &task.CurrentAttempt, &task.CrawlID, &task.PipelineVersion)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoTasksAvailable
		}
		return nil, err
	}

	leaseUntil := time.Now().Add(leaseDuration)
	nextAttempt := task.CurrentAttempt + 1

	// Update task status
	_, err = tx.ExecContext(ctx, `
		UPDATE tasks 
		SET status = 'LEASED', assigned_worker_id = $1, lease_until = $2, current_attempt = $3, started_at = COALESCE(started_at, NOW()), updated_at = NOW()
		WHERE id = $4
	`, workerID, leaseUntil, nextAttempt, task.ID)
	if err != nil {
		return nil, err
	}

	// Insert task attempt
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_attempts (task_id, worker_id, attempt_number, status, lease_until)
		VALUES ($1, $2, $3, 'RUNNING', $4)
	`, task.ID, workerID, nextAttempt, leaseUntil)
	if err != nil {
		return nil, err
	}

	// Update worker state to BUSY
	_, err = tx.ExecContext(ctx, `
		UPDATE workers 
		SET status = 'BUSY', current_task_id = $1, updated_at = NOW()
		WHERE worker_id = $2
	`, task.ID, workerID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &ClaimResult{
		TaskID:          task.TaskID,
		Dataset:         task.Dataset,
		SourcePath:      task.SourcePath,
		CrawlID:         task.CrawlID,
		PipelineVersion: task.PipelineVersion,
		LeaseUntil:      leaseUntil,
	}, nil
}

// RenewLease extends the lease duration of a task.
func (r *Repository) RenewLease(ctx context.Context, taskID string, workerID string, leaseDuration time.Duration) (time.Time, error) {
	leaseUntil := time.Now().Add(leaseDuration)

	res, err := r.db.ExecContext(ctx, `
		UPDATE tasks 
		SET lease_until = $1, updated_at = NOW()
		WHERE task_id = $2 AND assigned_worker_id = $3 AND status = 'LEASED'
	`, leaseUntil, taskID, workerID)

	if err != nil {
		return time.Time{}, err
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		return time.Time{}, errors.New("task not found or not owned by worker")
	}

	// Also update attempt
	_, _ = r.db.ExecContext(ctx, `
		UPDATE task_attempts 
		SET lease_until = $1 
		WHERE task_id = (SELECT id FROM tasks WHERE task_id = $2) AND worker_id = $3 AND status = 'RUNNING'
	`, leaseUntil, taskID, workerID)

	return leaseUntil, nil
}

type CompleteData struct {
	TaskID           string
	WorkerID         string
	AttemptID        string
	ProcessedBytes   int64
	ProcessedRecords int
	LinksFound       int
	BacklinksFound   int
	OutputURI        string
}

// CompleteTask marks a task as successfully completed.
func (r *Repository) CompleteTask(ctx context.Context, data CompleteData) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var internalTaskID string
	var currentAttempt int
	err = tx.QueryRowContext(ctx, `
		SELECT id, current_attempt FROM tasks WHERE task_id = $1 AND assigned_worker_id = $2
	`, data.TaskID, data.WorkerID).Scan(&internalTaskID, &currentAttempt)

	if err != nil {
		return errors.New("invalid task or worker ownership")
	}

	// Update task_attempts
	_, err = tx.ExecContext(ctx, `
		UPDATE task_attempts 
		SET status = 'SUCCEEDED', finished_at = NOW(), processed_bytes = $1, processed_records = $2, processed_links = $3, output_uri = $4
		WHERE task_id = $5 AND attempt_number = $6
	`, data.ProcessedBytes, data.ProcessedRecords, data.LinksFound, data.OutputURI, internalTaskID, currentAttempt)
	if err != nil {
		return err
	}

	// Update tasks
	_, err = tx.ExecContext(ctx, `
		UPDATE tasks 
		SET status = 'SUCCEEDED', finished_at = NOW(), output_uri = $1, updated_at = NOW()
		WHERE id = $2
	`, data.OutputURI, internalTaskID)
	if err != nil {
		return err
	}

	// Update worker to READY
	_, err = tx.ExecContext(ctx, `
		UPDATE workers 
		SET status = 'READY', current_task_id = NULL, updated_at = NOW()
		WHERE worker_id = $1
	`, data.WorkerID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

type FailData struct {
	TaskID       string
	WorkerID     string
	AttemptID    string
	ErrorCode    string
	ErrorMessage string
	Retryable    bool
	MaxAttempts  int
}

// FailTask marks a task as failed and handles retries.
func (r *Repository) FailTask(ctx context.Context, data FailData) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var internalTaskID string
	var currentAttempt int
	err = tx.QueryRowContext(ctx, `
		SELECT id, current_attempt FROM tasks WHERE task_id = $1 AND assigned_worker_id = $2
	`, data.TaskID, data.WorkerID).Scan(&internalTaskID, &currentAttempt)

	if err != nil {
		return errors.New("invalid task or worker ownership")
	}

	// Calculate next status
	nextStatus := "FAILED"
	if data.Retryable && currentAttempt < data.MaxAttempts {
		nextStatus = "QUEUED"
	}

	// Update task_attempts
	_, err = tx.ExecContext(ctx, `
		UPDATE task_attempts 
		SET status = 'FAILED', finished_at = NOW(), error_code = $1, error_message = $2
		WHERE task_id = $3 AND attempt_number = $4
	`, data.ErrorCode, data.ErrorMessage, internalTaskID, currentAttempt)
	if err != nil {
		return err
	}

	// Update tasks
	_, err = tx.ExecContext(ctx, `
		UPDATE tasks 
		SET status = $1, assigned_worker_id = NULL, lease_until = NULL, updated_at = NOW()
		WHERE id = $2
	`, nextStatus, internalTaskID)
	if err != nil {
		return err
	}

	// Update worker to READY
	_, err = tx.ExecContext(ctx, `
		UPDATE workers 
		SET status = 'READY', current_task_id = NULL, updated_at = NOW()
		WHERE worker_id = $1
	`, data.WorkerID)
	if err != nil {
		return err
	}

	return tx.Commit()
}
