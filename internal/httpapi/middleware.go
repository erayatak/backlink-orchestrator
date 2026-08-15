package httpapi

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/backlink-orchestrator/internal/auth"
	"github.com/backlink-orchestrator/internal/database"
)

type WorkerContextKey string

const WorkerIDKey WorkerContextKey = "worker_id"

// WorkerAuthMiddleware authenticates requests using the long-lived worker token.
func WorkerAuthMiddleware(db *database.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := auth.ExtractBearerToken(r)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid authorization token", "")
				return
			}

			tokenHash := auth.HashToken(token)

			var workerID, status string
			err = db.QueryRowContext(r.Context(), `
				SELECT worker_id, status 
				FROM workers 
				WHERE token_hash = $1
			`, tokenHash).Scan(&workerID, &status)

			if err != nil {
				if err == sql.ErrNoRows {
					WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid token", "")
					return
				}
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Database error", "")
				return
			}

			if status == "DISABLED" {
				WriteError(w, http.StatusForbidden, "FORBIDDEN", "Worker is disabled", "")
				return
			}

			// Add worker_id to context
			ctx := context.WithValue(r.Context(), WorkerIDKey, workerID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
