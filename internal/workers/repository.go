package workers

import (
	"context"
	"database/sql"
	"errors"

	"github.com/backlink-orchestrator/internal/database"
)

type Repository struct {
	db *database.DB
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

var ErrInvalidBootstrapToken = errors.New("invalid or expired bootstrap token")

// ConsumeBootstrapToken verifies and marks a bootstrap token as used within a transaction.
func (r *Repository) ConsumeBootstrapToken(ctx context.Context, tokenHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM bootstrap_tokens 
		WHERE token_hash = $1 
		  AND status = 'ACTIVE' 
		  AND used_at IS NULL 
		  AND expires_at > NOW()
		FOR UPDATE
	`, tokenHash).Scan(&id)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidBootstrapToken
		}
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE bootstrap_tokens 
		SET status = 'USED', used_at = NOW() 
		WHERE id = $1
	`, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

type Worker struct {
	WorkerID     string
	Hostname     string
	OS           string
	Architecture string
	CPUCount     int
	MemoryMB     int
	Version      string
	TokenHash    string
}

// RegisterWorker inserts or updates a worker in the database.
func (r *Repository) RegisterWorker(ctx context.Context, w Worker) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO workers (worker_id, status, hostname, os, architecture, cpu_count, memory_mb, version, token_hash, last_heartbeat_at)
		VALUES ($1, 'READY', $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (worker_id) DO UPDATE SET
			status = 'READY',
			hostname = EXCLUDED.hostname,
			os = EXCLUDED.os,
			architecture = EXCLUDED.architecture,
			cpu_count = EXCLUDED.cpu_count,
			memory_mb = EXCLUDED.memory_mb,
			version = EXCLUDED.version,
			token_hash = EXCLUDED.token_hash,
			last_heartbeat_at = NOW(),
			updated_at = NOW(),
			disabled_at = NULL
	`, w.WorkerID, w.Hostname, w.OS, w.Architecture, w.CPUCount, w.MemoryMB, w.Version, w.TokenHash)
	return err
}

type HeartbeatData struct {
	WorkerID         string
	Status           string
	CurrentTaskID    *string
	CPUPercent       float64
	MemoryPercent    float64
	TasksCompleted   int
	ProcessedRecords int
	ProcessedLinks   int
}

// RecordHeartbeat updates the worker's last heartbeat and records history.
func (r *Repository) RecordHeartbeat(ctx context.Context, data HeartbeatData) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update worker
	_, err = tx.ExecContext(ctx, `
		UPDATE workers 
		SET status = $1, 
		    current_task_id = (SELECT id FROM tasks WHERE task_id = $2), 
		    last_heartbeat_at = NOW()
		WHERE worker_id = $3
	`, data.Status, data.CurrentTaskID, data.WorkerID)
	if err != nil {
		return err
	}

	// Insert history
	_, err = tx.ExecContext(ctx, `
		INSERT INTO worker_heartbeat_history 
		(worker_id, status, cpu_percent, memory_percent, current_task_id, processed_records, processed_links)
		VALUES ($1, $2, $3, $4, (SELECT id FROM tasks WHERE task_id = $5), $6, $7)
	`, data.WorkerID, data.Status, data.CPUPercent, data.MemoryPercent, data.CurrentTaskID, data.ProcessedRecords, data.ProcessedLinks)
	if err != nil {
		return err
	}

	return tx.Commit()
}
