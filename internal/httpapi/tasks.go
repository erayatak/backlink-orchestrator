package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/tasks"
)

type TaskHandler struct {
	repo *tasks.Repository
	cfg  *config.Config
}

func NewTaskHandler(repo *tasks.Repository, cfg *config.Config) *TaskHandler {
	return &TaskHandler{repo: repo, cfg: cfg}
}

type ClaimRequest struct {
	WorkerID string `json:"worker_id"`
	Capacity struct {
		CPUCount int `json:"cpu_count"`
		MemoryMB int `json:"memory_mb"`
	} `json:"capacity"`
}

type ClaimResponse struct {
	TaskID     string `json:"task_id"`
	LeaseUntil string `json:"lease_until"`
	Type       string `json:"type"`
	Source     struct {
		Dataset string `json:"dataset"`
		Path    string `json:"path"`
	} `json:"source"`
}

func (h *TaskHandler) Claim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workerIDContext := ctx.Value(WorkerIDKey)

	var req ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload", "")
		return
	}

	if workerIDContext != nil && workerIDContext.(string) != req.WorkerID {
		WriteError(w, http.StatusForbidden, "FORBIDDEN", "Token does not match worker_id", "")
		return
	}

	res, err := h.repo.ClaimTask(ctx, req.WorkerID, h.cfg.TaskLeaseDuration)
	if err != nil {
		if err == tasks.ErrNoTasksAvailable {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		slog.Error("Failed to claim task", "error", err.Error(), "worker_id", req.WorkerID)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to claim task", "")
		return
	}

	resp := ClaimResponse{
		TaskID:     res.TaskID,
		LeaseUntil: res.LeaseUntil.UTC().Format(time.RFC3339),
		Type:       "COMMON_CRAWL_WAT",
	}
	resp.Source.Dataset = res.Dataset
	resp.Source.Path = res.SourcePath

	WriteJSON(w, http.StatusOK, resp)
}

type TaskHeartbeatResponse struct {
	LeaseUntil string `json:"lease_until"`
}

func (h *TaskHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workerID := ctx.Value(WorkerIDKey).(string)
	taskID := r.PathValue("task_id")

	// Payload could contain progress metrics, but we only strictly need task_id and auth

	newLease, err := h.repo.RenewLease(ctx, taskID, workerID, h.cfg.TaskLeaseDuration)
	if err != nil {
		slog.Error("Failed to renew task lease", "error", err.Error(), "task_id", taskID)
		WriteError(w, http.StatusConflict, "LEASE_FAILED", "Could not renew lease", "")
		return
	}

	WriteJSON(w, http.StatusOK, TaskHeartbeatResponse{
		LeaseUntil: newLease.UTC().Format(time.RFC3339),
	})
}

type CompleteRequest struct {
	AttemptID        string `json:"attempt_id"`
	ProcessedBytes   int64  `json:"processed_bytes"`
	RecordsProcessed int    `json:"records_processed"`
	LinksFound       int    `json:"links_found"`
	BacklinksFound   int    `json:"backlinks_found"`
	OutputURI        string `json:"output_uri"`
}

func (h *TaskHandler) Complete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workerID := ctx.Value(WorkerIDKey).(string)
	taskID := r.PathValue("task_id")

	var req CompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload", "")
		return
	}

	if req.OutputURI == "" {
		WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "output_uri is required", "")
		return
	}

	data := tasks.CompleteData{
		TaskID:           taskID,
		WorkerID:         workerID,
		AttemptID:        req.AttemptID,
		ProcessedBytes:   req.ProcessedBytes,
		ProcessedRecords: req.RecordsProcessed,
		LinksFound:       req.LinksFound,
		BacklinksFound:   req.BacklinksFound,
		OutputURI:        req.OutputURI,
	}

	if err := h.repo.CompleteTask(ctx, data); err != nil {
		slog.Error("Failed to complete task", "error", err.Error(), "task_id", taskID)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to complete task", "")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

type FailRequest struct {
	AttemptID    string `json:"attempt_id"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	Retryable    bool   `json:"retryable"`
}

func (h *TaskHandler) Fail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workerID := ctx.Value(WorkerIDKey).(string)
	taskID := r.PathValue("task_id")

	var req FailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload", "")
		return
	}

	data := tasks.FailData{
		TaskID:       taskID,
		WorkerID:     workerID,
		AttemptID:    req.AttemptID,
		ErrorCode:    req.ErrorCode,
		ErrorMessage: req.ErrorMessage,
		Retryable:    req.Retryable,
		MaxAttempts:  h.cfg.TaskMaxAttempts,
	}

	if err := h.repo.FailTask(ctx, data); err != nil {
		slog.Error("Failed to mark task as failed", "error", err.Error(), "task_id", taskID)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process task failure", "")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
