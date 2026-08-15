package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/dashboard"
	"github.com/backlink-orchestrator/internal/database"
	"github.com/backlink-orchestrator/migrations"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestHealthAndAdmin(t *testing.T) {
	// Change working directory to project root for template loading
	os.Chdir("../../")

	cfg := config.Load()
	db, _ := database.Connect(context.Background(), cfg.DatabaseURL)
	defer db.Close()
	db.Migrate(context.Background(), migrations.FS)

	mux := http.NewServeMux()

	// Health and Metrics
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	})
	mux.HandleFunc("GET /metrics", promhttp.Handler().ServeHTTP)

	// Dashboard Auth
	dashHandler := dashboard.NewHandler(db, cfg)
	mux.HandleFunc("GET /login", dashHandler.LoginGet)

	authMux := http.NewServeMux()
	authMux.HandleFunc("GET /", dashHandler.Index)
	mux.Handle("/", dashHandler.AuthMiddleware(authMux))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Test /health/live
	respLive, _ := http.Get(srv.URL + "/health/live")
	if respLive.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /health/live, got %d", respLive.StatusCode)
	}

	// Test /health/ready
	respReady, _ := http.Get(srv.URL + "/health/ready")
	if respReady.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /health/ready, got %d", respReady.StatusCode)
	}

	// Test /metrics
	respMetrics, _ := http.Get(srv.URL + "/metrics")
	if respMetrics.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /metrics, got %d", respMetrics.StatusCode)
	}

	// Test Dashboard Auth
	// Requesting / should redirect to /login
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	respAuth, _ := client.Get(srv.URL + "/")
	if respAuth.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect to /login, got %d", respAuth.StatusCode)
	}
	if respAuth.Header.Get("Location") != "/login" {
		t.Errorf("expected Location /login, got %s", respAuth.Header.Get("Location"))
	}

	// Test Login page
	respLogin, _ := client.Get(srv.URL + "/login")
	if respLogin.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /login, got %d", respLogin.StatusCode)
	}
}
