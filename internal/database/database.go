package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type DB struct {
	*sql.DB
}

// Connect establishes a connection to the PostgreSQL database.
func Connect(ctx context.Context, dsn string) (*DB, error) {
	slog.Info("Connecting to PostgreSQL database")

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	slog.Info("Successfully connected to database")
	return &DB{db}, nil
}

// Close gracefully closes the database connection.
func (db *DB) Close() error {
	slog.Info("Closing database connection")
	return db.DB.Close()
}
