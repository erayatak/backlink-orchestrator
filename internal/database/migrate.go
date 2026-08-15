package database

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
)

// Migrate runs pending database migrations.
func (db *DB) Migrate(ctx context.Context, migrationFS fs.FS) error {
	slog.Info("Running database migrations")

	goose.SetBaseFS(migrationFS)
	goose.SetLogger(gooseLogger{})

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db.DB, "."); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slog.Info("Database migrations completed successfully")
	return nil
}

type gooseLogger struct{}

func (l gooseLogger) Fatalf(format string, v ...interface{}) {
	slog.Error(fmt.Sprintf(format, v...))
}

func (l gooseLogger) Printf(format string, v ...interface{}) {
	slog.Info(fmt.Sprintf(format, v...))
}
