package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/backlink-orchestrator/internal/database"
)

type Finalizer struct {
	db *database.DB
}

func NewFinalizer(db *database.DB) *Finalizer {
	return &Finalizer{db: db}
}

func (f *Finalizer) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.processFinalizingJobs(ctx)
		}
	}
}

func (f *Finalizer) processFinalizingJobs(ctx context.Context) {
	// Find jobs that are in FINALIZING status
	rows, err := f.db.QueryContext(ctx, `
		SELECT id, job_id, dataset, crawl_id, pipeline_version 
		FROM jobs 
		WHERE status = 'FINALIZING'
	`)
	if err != nil {
		slog.Error("Failed to query finalizing jobs", "error", err.Error())
		return
	}
	defer rows.Close()

	type JobInfo struct {
		ID              string
		JobID           string
		Dataset         string
		CrawlID         string
		PipelineVersion string
	}

	var jobsToFinalize []JobInfo
	for rows.Next() {
		var j JobInfo
		if err := rows.Scan(&j.ID, &j.JobID, &j.Dataset, &j.CrawlID, &j.PipelineVersion); err != nil {
			slog.Error("Failed to scan finalizing job", "error", err.Error())
			continue
		}
		jobsToFinalize = append(jobsToFinalize, j)
	}

	// For each job, we could generate a manifest, trigger MapReduce, or push to ClickHouse.
	// As per instructions, we just consolidate and mark COMPLETED/PARTIAL.
	for _, j := range jobsToFinalize {
		f.finalizeJob(ctx, j.ID)
	}
}

func (f *Finalizer) finalizeJob(ctx context.Context, internalJobID string) {
	slog.Info("Finalizing job", "job_id", internalJobID)

	// Check for any FAILED tasks to determine COMPLETED vs PARTIAL
	var failedTasks int
	err := f.db.QueryRowContext(ctx, "SELECT failed_tasks FROM jobs WHERE id = $1", internalJobID).Scan(&failedTasks)
	if err != nil {
		slog.Error("Failed to get job failed tasks", "job_id", internalJobID, "error", err.Error())
		return
	}

	nextStatus := "COMPLETED"
	if failedTasks > 0 {
		nextStatus = "PARTIAL"
	}

	// Update job status to final
	_, err = f.db.ExecContext(ctx, `
		UPDATE jobs 
		SET status = $1, finished_at = NOW(), updated_at = NOW() 
		WHERE id = $2
	`, nextStatus, internalJobID)

	if err != nil {
		slog.Error("Failed to finalize job state", "job_id", internalJobID, "error", err.Error())
	} else {
		slog.Info("Job finalized successfully", "job_id", internalJobID, "status", nextStatus)
	}
}
