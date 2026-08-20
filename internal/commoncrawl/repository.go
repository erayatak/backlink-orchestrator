package commoncrawl

import (
	"context"
	"database/sql"
	"fmt"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) UpsertArchive(ctx context.Context, crawlID, displayName string) error {
	query := `
		INSERT INTO crawl_archives (crawl_id, display_name, status, discovered_at, last_sync_at)
		VALUES ($1, $2, 'DISCOVERED', NOW(), NOW())
		ON CONFLICT (crawl_id) DO UPDATE SET 
			display_name = EXCLUDED.display_name,
			last_sync_at = NOW()
	`
	_, err := r.db.ExecContext(ctx, query, crawlID, displayName)
	return err
}

func (r *Repository) GetArchives(ctx context.Context) ([]CatalogEntry, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT crawl_id, display_name FROM crawl_archives ORDER BY crawl_id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []CatalogEntry
	for rows.Next() {
		var e CatalogEntry
		if err := rows.Scan(&e.ID, &e.Name); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *Repository) UpsertWATPaths(ctx context.Context, crawlID string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Using a prepared statement for batch inserts to avoid massive query strings
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO crawl_files (crawl_id, path, status, created_at)
		VALUES ($1, $2, 'DISCOVERED', NOW())
		ON CONFLICT (crawl_id, path) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range paths {
		if _, err := stmt.ExecContext(ctx, crawlID, p); err != nil {
			return fmt.Errorf("failed to insert path %s: %w", p, err)
		}
	}

	// Update the archive status to READY if it has files
	_, err = tx.ExecContext(ctx, "UPDATE crawl_archives SET status = 'READY' WHERE crawl_id = $1", crawlID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) HasWATPaths(ctx context.Context, crawlID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM crawl_files WHERE crawl_id = $1", crawlID).Scan(&count)
	return count > 0, err
}
