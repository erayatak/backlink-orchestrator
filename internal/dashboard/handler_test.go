package dashboard_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/backlink-orchestrator/internal/auth"
	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/dashboard"
	"github.com/backlink-orchestrator/internal/database"
	"github.com/backlink-orchestrator/migrations"
)

func setupTestDB(t *testing.T) *database.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres@localhost:5432/testdb?sslmode=disable"
	}
	
	schemaName := "dashboard_test"
	if strings.Contains(dbURL, "?") {
		dbURL = dbURL + "&search_path=" + schemaName
	} else {
		dbURL = dbURL + "?search_path=" + schemaName
	}

	// Create schema using a temporary connection first
	tempDB, err := database.Connect(context.Background(), strings.Replace(dbURL, "&search_path="+schemaName, "", 1))
	if err == nil {
		_, _ = tempDB.Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE; CREATE SCHEMA " + schemaName + ";")
		tempDB.Close()
	}

	db, err := database.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}

	err = db.Migrate(context.Background(), migrations.FS)
	if err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	return db
}

func setupTestApp(t *testing.T, db *database.DB) (*httptest.Server, *http.Cookie) {
	// Change to root for templates
	_ = os.Chdir("../../")

	cfg := &config.Config{
		AdminUsername: "admin",
		AdminPassword: "password",
	}

	dashHandler := dashboard.NewHandler(db, cfg)

	mux := http.NewServeMux()
	authMux := http.NewServeMux()

	authMux.HandleFunc("GET /workers", dashHandler.Workers)
	authMux.HandleFunc("GET /workers/list/data", dashHandler.WorkersData)

	mux.Handle("/", dashHandler.AuthMiddleware(authMux))

	srv := httptest.NewServer(mux)

	// Create valid session manually
	adminAuth := auth.NewAdminAuth(db)
	session, err := adminAuth.GenerateSession(context.Background(), "admin")
	if err != nil {
		t.Fatalf("failed to generate session: %v", err)
	}

	cookie := &http.Cookie{
		Name:  "admin_session",
		Value: session.SessionID,
	}

	return srv, cookie
}

func TestDashboardWorkers(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer os.Chdir("internal/dashboard") // Restore dir just in case

	srv, cookie := setupTestApp(t, db)
	defer srv.Close()

	client := &http.Client{}

	makeReq := func(url string) (*http.Response, string) {
		req, _ := http.NewRequest("GET", url, nil)
		req.AddCookie(cookie)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(body)
	}

	// 1. Empty workers table should render correctly
	resp, body := makeReq(srv.URL + "/workers/list/data")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "No workers registered yet") {
		t.Errorf("expected empty state message, got: %s", body)
	}

	// GET /workers works (returns layout)
	respW, _ := makeReq(srv.URL + "/workers")
	if respW.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /workers, got %d", respW.StatusCode)
	}

	// 2. Insert real worker
	_, err := db.Exec(`
		INSERT INTO workers (worker_id, status, hostname, os, architecture, version, token_hash, cpu_count, memory_mb, current_task_id, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, "worker_init_123", "READY", "instance-1", "linux", "amd64", "1.0.0", "hash", 4, 1024, nil, time.Now())
	if err != nil {
		t.Fatalf("insert worker failed: %v", err)
	}

	// 3. Render real worker row
	respReal, bodyReal := makeReq(srv.URL + "/workers/list/data")
	if respReal.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", respReal.StatusCode)
	}
	if !strings.Contains(bodyReal, "worker_init_123") {
		t.Errorf("expected worker id in body, got: %s", bodyReal)
	}

	// 4. Force a scan error (e.g. drop table) to surface the error handling
	db.Exec("DROP TABLE workers CASCADE")
	respErr, bodyErr := makeReq(srv.URL + "/workers/list/data")
	if respErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 on DB error, got %d", respErr.StatusCode)
	}
	if !strings.Contains(bodyErr, "Internal server error") {
		t.Errorf("expected generic error message, got: %s", bodyErr)
	}
}
