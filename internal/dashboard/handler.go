package dashboard

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/backlink-orchestrator/internal/auth"
	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/database"
)

type Handler struct {
	db        *database.DB
	cfg       *config.Config
	tmpl      *template.Template
	adminAuth *auth.AdminAuth
}

func NewHandler(db *database.DB, cfg *config.Config) *Handler {
	funcs := template.FuncMap{
		"Percent": func(part, total int) float64 {
			if total == 0 {
				return 0
			}
			return float64(part) / float64(total) * 100
		},
	}

	t := template.Must(template.New("layout").Funcs(funcs).ParseGlob("web/templates/*.html"))
	adminAuth := auth.NewAdminAuth(db)

	return &Handler{db: db, cfg: cfg, tmpl: t, adminAuth: adminAuth}
}

// AuthMiddleware requires a valid session cookie
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("admin_session")
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		session, err := h.adminAuth.VerifySession(r.Context(), cookie.Value)
		if err != nil {
			// clear cookie
			http.SetCookie(w, &http.Cookie{
				Name:     "admin_session",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
			})
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), "admin_session", session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) LoginGet(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "login.html", nil)
}

func (h *Handler) LoginPost(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	// For MVP: Check against config directly
	if username != h.cfg.AdminUsername {
		h.tmpl.ExecuteTemplate(w, "login.html", map[string]string{"Error": "Invalid credentials"})
		return
	}

	valid, err := auth.VerifyAdminPassword(password, h.cfg.AdminPassword)
	if err != nil || !valid {
		h.tmpl.ExecuteTemplate(w, "login.html", map[string]string{"Error": "Invalid credentials"})
		return
	}

	session, err := h.adminAuth.GenerateSession(r.Context(), username)
	if err != nil {
		h.tmpl.ExecuteTemplate(w, "login.html", map[string]string{"Error": "Internal error generating session"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    session.SessionID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) LogoutPost(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("admin_session")
	if err == nil && cookie.Value != "" {
		_ = h.adminAuth.ClearSession(r.Context(), cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "layout.html", "/overview")
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	var totalWorkers, onlineWorkers, offlineWorkers int
	var queuedTasks, runningTasks, succeededTasks, failedTasks int
	var activeJobs, completedJobs, failedJobs int

	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM workers").Scan(&totalWorkers)
	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM workers WHERE status IN ('READY', 'BUSY')").Scan(&onlineWorkers)
	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM workers WHERE status = 'OFFLINE'").Scan(&offlineWorkers)

	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM tasks WHERE status = 'QUEUED'").Scan(&queuedTasks)
	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM tasks WHERE status IN ('LEASED', 'RUNNING')").Scan(&runningTasks)
	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM tasks WHERE status = 'SUCCEEDED'").Scan(&succeededTasks)
	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM tasks WHERE status = 'FAILED'").Scan(&failedTasks)

	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM jobs WHERE status IN ('QUEUED', 'RUNNING', 'PAUSED')").Scan(&activeJobs)
	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM jobs WHERE status = 'COMPLETED'").Scan(&completedJobs)
	h.db.QueryRowContext(r.Context(), "SELECT count(*) FROM jobs WHERE status = 'FAILED'").Scan(&failedJobs)

	data := struct {
		TotalWorkers   int
		OnlineWorkers  int
		OfflineWorkers int
		QueuedTasks    int
		RunningTasks   int
		SucceededTasks int
		FailedTasks    int
		ActiveJobs     int
		CompletedJobs  int
		FailedJobs     int
	}{
		totalWorkers, onlineWorkers, offlineWorkers,
		queuedTasks, runningTasks, succeededTasks, failedTasks,
		activeJobs, completedJobs, failedJobs,
	}

	h.tmpl.ExecuteTemplate(w, "overview.html", data)
}

func (h *Handler) Workers(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		h.WorkersData(w, r)
		return
	}
	// Full page load via index which fetches via hx-get
	h.tmpl.ExecuteTemplate(w, "layout.html", "/workers")
}

func (h *Handler) WorkersData(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT worker_id, status, hostname, version, cpu_count, memory_mb, current_task_id, last_heartbeat_at 
		FROM workers ORDER BY last_heartbeat_at DESC LIMIT 50
	`)
	if err != nil {
		slog.Error("Failed to query workers", "error", err.Error())
		http.Error(w, "Internal server error querying workers", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type WorkerRow struct {
		WorkerID        string
		Status          string
		Hostname        string
		Version         string
		CpuCount        int
		MemoryMb        int
		CurrentTaskID   *string
		LastHeartbeatAt time.Time
	}

	var workers []WorkerRow
	for rows.Next() {
		var row WorkerRow
		err := rows.Scan(
			&row.WorkerID,
			&row.Status,
			&row.Hostname,
			&row.Version,
			&row.CpuCount,
			&row.MemoryMb,
			&row.CurrentTaskID,
			&row.LastHeartbeatAt,
		)
		if err != nil {
			slog.Error("Failed to scan worker row", "error", err.Error())
			http.Error(w, "Internal server error scanning worker row", http.StatusInternalServerError)
			return
		}
		workers = append(workers, row)
	}

	if err := rows.Err(); err != nil {
		slog.Error("Rows iteration error in workers", "error", err.Error())
		http.Error(w, "Internal server error reading workers", http.StatusInternalServerError)
		return
	}

	h.tmpl.ExecuteTemplate(w, "workers.html", map[string]interface{}{"Workers": workers})
}

func (h *Handler) Tasks(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		h.TasksData(w, r)
		return
	}
	h.tmpl.ExecuteTemplate(w, "layout.html", "/tasks")
}

func (h *Handler) TasksData(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT t.task_id, j.job_id, t.status, t.source_path, t.assigned_worker_id, t.current_attempt
		FROM tasks t
		JOIN jobs j ON t.job_id = j.id
		ORDER BY t.created_at DESC LIMIT 50
	`)
	if err != nil {
		slog.Error("Failed to query tasks", "error", err.Error())
		http.Error(w, "Internal server error querying tasks", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type TaskRow struct {
		TaskID           string
		JobID            string
		Status           string
		SourcePath       string
		AssignedWorkerID *string
		CurrentAttempt   int
	}

	var tasks []TaskRow
	for rows.Next() {
		var t TaskRow
		err := rows.Scan(
			&t.TaskID,
			&t.JobID,
			&t.Status,
			&t.SourcePath,
			&t.AssignedWorkerID,
			&t.CurrentAttempt,
		)
		if err != nil {
			slog.Error("Failed to scan task row", "error", err.Error())
			http.Error(w, "Internal server error scanning task row", http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		slog.Error("Rows iteration error in tasks", "error", err.Error())
		http.Error(w, "Internal server error reading tasks", http.StatusInternalServerError)
		return
	}

	h.tmpl.ExecuteTemplate(w, "tasks.html", map[string]interface{}{"Tasks": tasks})
}

func (h *Handler) Jobs(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		h.JobsData(w, r)
		return
	}
	h.tmpl.ExecuteTemplate(w, "layout.html", "/jobs")
}

func (h *Handler) JobsData(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT job_id, dataset, crawl_id, status, total_tasks, succeeded_tasks, failed_tasks
		FROM jobs ORDER BY created_at DESC LIMIT 50
	`)
	if err != nil {
		slog.Error("Failed to query jobs", "error", err.Error())
		http.Error(w, "Internal server error querying jobs", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type JobRow struct {
		JobID          string
		Dataset        string
		CrawlID        string
		Status         string
		TotalTasks     int
		SucceededTasks int
		FailedTasks    int
	}

	var jobs []JobRow
	for rows.Next() {
		var j JobRow
		err := rows.Scan(
			&j.JobID,
			&j.Dataset,
			&j.CrawlID,
			&j.Status,
			&j.TotalTasks,
			&j.SucceededTasks,
			&j.FailedTasks,
		)
		if err != nil {
			slog.Error("Failed to scan job row", "error", err.Error())
			http.Error(w, "Internal server error scanning job row", http.StatusInternalServerError)
			return
		}
		jobs = append(jobs, j)
	}

	if err := rows.Err(); err != nil {
		slog.Error("Rows iteration error in jobs", "error", err.Error())
		http.Error(w, "Internal server error reading jobs", http.StatusInternalServerError)
		return
	}

	h.tmpl.ExecuteTemplate(w, "jobs.html", map[string]interface{}{"Jobs": jobs})
}
