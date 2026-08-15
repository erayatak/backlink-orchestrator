package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/backlink-orchestrator/internal/auth"
	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/workers"
)

type WorkerHandler struct {
	repo *workers.Repository
	cfg  *config.Config
}

func NewWorkerHandler(repo *workers.Repository, cfg *config.Config) *WorkerHandler {
	return &WorkerHandler{repo: repo, cfg: cfg}
}

type RegisterRequest struct {
	WorkerID     string `json:"worker_id"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	CPUCount     int    `json:"cpu_count"`
	MemoryMB     int    `json:"memory_mb"`
	Version      string `json:"version"`
}

type RegisterResponse struct {
	WorkerID                 string `json:"worker_id"`
	Status                   string `json:"status"`
	ServerTime               string `json:"server_time"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
	HeartbeatTimeoutSeconds  int    `json:"heartbeat_timeout_seconds"`
	Token                    string `json:"token"`
}

func (h *WorkerHandler) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Extract Bootstrap Token
	bootstrapToken, err := auth.ExtractBearerToken(r)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error(), "")
		return
	}

	// 2. Consume Bootstrap Token
	tokenHash := auth.HashToken(bootstrapToken)
	if err := h.repo.ConsumeBootstrapToken(ctx, tokenHash); err != nil {
		if err == workers.ErrInvalidBootstrapToken {
			WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "Bootstrap token is invalid, expired, or already used", "")
			return
		}
		slog.Error("Failed to consume bootstrap token", "error", err.Error())
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error", "")
		return
	}

	// 3. Parse Request
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload", "")
		return
	}

	// Basic validation
	if req.WorkerID == "" || req.Hostname == "" {
		WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "worker_id and hostname are required", "")
		return
	}

	// 4. Generate Long-Lived Token
	newToken, err := auth.GenerateToken(32)
	if err != nil {
		slog.Error("Failed to generate token", "error", err.Error())
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate worker credential", "")
		return
	}

	// 5. Save Worker
	worker := workers.Worker{
		WorkerID:     req.WorkerID,
		Hostname:     req.Hostname,
		OS:           req.OS,
		Architecture: req.Architecture,
		CPUCount:     req.CPUCount,
		MemoryMB:     req.MemoryMB,
		Version:      req.Version,
		TokenHash:    auth.HashToken(newToken),
	}

	if err := h.repo.RegisterWorker(ctx, worker); err != nil {
		slog.Error("Failed to register worker", "error", err.Error())
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to register worker", "")
		return
	}

	// 6. Return Response
	slog.Info("Worker registered successfully", "worker_id", req.WorkerID, "hostname", req.Hostname)

	resp := RegisterResponse{
		WorkerID:                 req.WorkerID,
		Status:                   "READY",
		ServerTime:               time.Now().UTC().Format(time.RFC3339),
		HeartbeatIntervalSeconds: int(h.cfg.HeartbeatInterval.Seconds()),
		HeartbeatTimeoutSeconds:  int(h.cfg.HeartbeatTimeout.Seconds()),
		Token:                    newToken,
	}

	WriteJSON(w, http.StatusOK, resp)
}

type HeartbeatRequest struct {
	WorkerID         string  `json:"worker_id"`
	Status           string  `json:"status"`
	CurrentTaskID    string  `json:"current_task_id,omitempty"`
	CPUPercent       float64 `json:"cpu_percent"`
	MemoryPercent    float64 `json:"memory_percent"`
	TasksCompleted   int     `json:"tasks_completed"`
	ProcessedRecords int     `json:"records_processed"`
	ProcessedLinks   int     `json:"links_extracted"`
}

type HeartbeatResponse struct {
	OK         bool   `json:"ok"`
	ServerTime string `json:"server_time"`
	TaskAction string `json:"task_action"`
}

func (h *WorkerHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workerIDContext := ctx.Value(WorkerIDKey)

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload", "")
		return
	}

	if workerIDContext != nil && workerIDContext.(string) != req.WorkerID {
		WriteError(w, http.StatusForbidden, "FORBIDDEN", "Token does not match worker_id", "")
		return
	}

	var taskID *string
	if req.CurrentTaskID != "" {
		taskID = &req.CurrentTaskID
	}

	data := workers.HeartbeatData{
		WorkerID:         req.WorkerID,
		Status:           req.Status,
		CurrentTaskID:    taskID,
		CPUPercent:       req.CPUPercent,
		MemoryPercent:    req.MemoryPercent,
		TasksCompleted:   req.TasksCompleted,
		ProcessedRecords: req.ProcessedRecords,
		ProcessedLinks:   req.ProcessedLinks,
	}

	if err := h.repo.RecordHeartbeat(ctx, data); err != nil {
		slog.Error("Failed to record heartbeat", "error", err.Error(), "worker_id", req.WorkerID)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to record heartbeat", "")
		return
	}

	resp := HeartbeatResponse{
		OK:         true,
		ServerTime: time.Now().UTC().Format(time.RFC3339),
		TaskAction: "CONTINUE", // In the future, this could be ABORT if task was reassigned
	}
	WriteJSON(w, http.StatusOK, resp)
}
