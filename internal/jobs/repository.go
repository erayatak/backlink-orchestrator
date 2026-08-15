package jobs

import (
	"context"
	"fmt"

	"github.com/backlink-orchestrator/internal/database"
)

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

type JobCreate struct {
	Dataset     string
	CrawlID     string
	SourcePaths []string
}

// CreateJob transactional creates a job and its associated tasks.
func (r *Repository) CreateJob(ctx context.Context, job JobCreate) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	jobID := fmt.Sprintf("%s-%s", job.Dataset, job.CrawlID)
	totalTasks := len(job.SourcePaths)

	var internalJobID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO jobs (job_id, dataset, crawl_id, status, total_tasks, queued_tasks)
		VALUES ($1, $2, $3, 'QUEUED', $4, $5)
		RETURNING id
	`, jobID, job.Dataset, job.CrawlID, totalTasks, totalTasks).Scan(&internalJobID)

	if err != nil {
		return fmt.Errorf("failed to insert job: %w", err)
	}

	// Insert tasks in batches or one-by-one.
	// For production we would use batching (e.g. pgx CopyFrom), but for MVP we can prepare a statement.
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO tasks (task_id, job_id, dataset, source_path, status)
		VALUES ($1, $2, $3, $4, 'QUEUED')
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare task insert: %w", err)
	}
	defer stmt.Close()

	for i, path := range job.SourcePaths {
		taskID := fmt.Sprintf("%s-task-%d", jobID, i+1)
		_, err = stmt.ExecContext(ctx, taskID, internalJobID, job.Dataset, path)
		if err != nil {
			return fmt.Errorf("failed to insert task %s: %w", path, err)
		}
	}

	return tx.Commit()
}

// UpdateJobStatus allows admins to pause, resume, or cancel jobs.
func (r *Repository) UpdateJobStatus(ctx context.Context, jobID string, status string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE jobs SET status = $1, updated_at = NOW() WHERE job_id = $2", status, jobID)
	return err
}

// UpdateJobProgress updates the progress counts of a job based on its tasks.
// In a real system, this could be triggered by database triggers or a background loop.
func (r *Repository) UpdateJobProgress(ctx context.Context, internalJobID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET 
			queued_tasks = (SELECT count(*) FROM tasks WHERE job_id = $1 AND status = 'QUEUED'),
			running_tasks = (SELECT count(*) FROM tasks WHERE job_id = $1 AND status IN ('LEASED', 'RUNNING')),
			succeeded_tasks = (SELECT count(*) FROM tasks WHERE job_id = $1 AND status = 'SUCCEEDED'),
			failed_tasks = (SELECT count(*) FROM tasks WHERE job_id = $1 AND status = 'FAILED'),
			updated_at = NOW()
		WHERE id = $1
	`, internalJobID)

	if err != nil {
		return err
	}

	// If all tasks are completed or failed, update job status
	_, err = r.db.ExecContext(ctx, `
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
		WHERE id = $1 AND status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED')
	`, internalJobID)

	return err
}
