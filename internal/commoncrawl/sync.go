package commoncrawl

import (
	"context"
	"log/slog"
	"time"
)

type Syncer struct {
	client *Client
	repo   *Repository
}

func NewSyncer(client *Client, repo *Repository) *Syncer {
	return &Syncer{client: client, repo: repo}
}

func (s *Syncer) Start(ctx context.Context, interval time.Duration) {
	// Initial sync immediately
	s.syncAll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAll(ctx)
		}
	}
}

func (s *Syncer) syncAll(ctx context.Context) {
	slog.Info("Starting Common Crawl catalog sync")
	catalog, err := s.client.FetchCatalog(ctx)
	if err != nil {
		slog.Error("Failed to fetch common crawl catalog", "error", err.Error())
		return
	}

	for _, entry := range catalog {
		if err := s.repo.UpsertArchive(ctx, entry.ID, entry.Name); err != nil {
			slog.Error("Failed to upsert crawl archive", "error", err.Error(), "crawl_id", entry.ID)
			continue
		}

		// Check if we already have WAT paths
		hasPaths, err := s.repo.HasWATPaths(ctx, entry.ID)
		if err != nil {
			slog.Error("Failed to check WAT paths", "error", err.Error(), "crawl_id", entry.ID)
			continue
		}

		if !hasPaths {
			slog.Info("Fetching WAT paths for new crawl", "crawl_id", entry.ID)
			paths, err := s.client.FetchWATPaths(ctx, entry.ID)
			if err != nil {
				slog.Error("Failed to fetch WAT paths", "error", err.Error(), "crawl_id", entry.ID)
				continue
			}

			if err := s.repo.UpsertWATPaths(ctx, entry.ID, paths); err != nil {
				slog.Error("Failed to upsert WAT paths", "error", err.Error(), "crawl_id", entry.ID)
			}
		}
	}
	slog.Info("Finished Common Crawl catalog sync")
}

func (s *Syncer) TriggerSync(ctx context.Context) {
	// Optional manual trigger
	go s.syncAll(ctx)
}
