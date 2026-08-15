package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/backlink-orchestrator/internal/database"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HttpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orchestrator_http_requests_total",
			Help: "Total HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HttpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "orchestrator_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	WorkersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "workers_total",
		Help: "Total registered workers",
	})
	WorkersOnline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "workers_online",
		Help: "Online workers",
	})
	WorkersOffline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "workers_offline",
		Help: "Offline workers",
	})

	TasksQueued = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tasks_queued",
		Help: "Queued tasks",
	})
	TasksRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tasks_running",
		Help: "Running tasks",
	})

	TasksSucceededTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tasks_succeeded_total",
		Help: "Total succeeded tasks",
	})
	TasksFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tasks_failed_total",
		Help: "Total failed tasks",
	})
	TasksRetriedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tasks_retried_total",
		Help: "Total retried tasks",
	})

	TaskClaimDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "task_claim_duration_seconds",
		Help:    "Duration to claim a task",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5},
	})
	TaskDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "task_duration_seconds",
		Help:    "Duration to complete a task",
		Buckets: []float64{1, 10, 60, 300, 600, 1800, 3600},
	})

	TaskRecoveryTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "task_recovery_total",
		Help: "Total tasks recovered due to lease expiration",
	})
	HeartbeatTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "heartbeat_total",
		Help: "Total heartbeats received",
	})
	HeartbeatTimeoutTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "heartbeat_timeout_total",
		Help: "Total heartbeats timed out",
	})
	DbQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "Database query duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})
)

// RecordMetrics starts a goroutine that periodically updates gauge metrics from the DB.
func StartMetricsUpdater(db *database.DB) {
	go func() {
		for {
			var total, online, offline int
			var queued, running int

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = db.QueryRowContext(ctx, "SELECT count(*) FROM workers").Scan(&total)
			_ = db.QueryRowContext(ctx, "SELECT count(*) FROM workers WHERE status IN ('READY', 'BUSY')").Scan(&online)
			_ = db.QueryRowContext(ctx, "SELECT count(*) FROM workers WHERE status = 'OFFLINE'").Scan(&offline)

			_ = db.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE status = 'QUEUED'").Scan(&queued)
			_ = db.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE status IN ('LEASED', 'RUNNING')").Scan(&running)
			cancel()

			WorkersTotal.Set(float64(total))
			WorkersOnline.Set(float64(online))
			WorkersOffline.Set(float64(offline))
			TasksQueued.Set(float64(queued))
			TasksRunning.Set(float64(running))

			time.Sleep(15 * time.Second)
		}
	}()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}

		next.ServeHTTP(rec, r)

		duration := time.Since(start).Seconds()
		path := r.URL.Path
		// Simplified path cardinality
		if len(path) > 30 {
			path = "/api/v1/..."
		}

		HttpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
		HttpRequestsTotal.WithLabelValues(r.Method, path, http.StatusText(rec.status)).Inc()
	})
}
