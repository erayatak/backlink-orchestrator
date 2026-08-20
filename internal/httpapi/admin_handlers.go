package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/backlink-orchestrator/internal/commoncrawl"
	"github.com/backlink-orchestrator/internal/jobs"
)

type AdminHandler struct {
	jobRepo *jobs.Repository
	ccRepo  *commoncrawl.Repository
}

func NewAdminHandler(jobRepo *jobs.Repository, ccRepo *commoncrawl.Repository) *AdminHandler {
	return &AdminHandler{jobRepo: jobRepo, ccRepo: ccRepo}
}

type StartScanRequest struct {
	PipelineVersion string `json:"pipeline_version"`
}

func (h *AdminHandler) StartScan(w http.ResponseWriter, r *http.Request) {
	crawlID := r.PathValue("crawl_id")

	var req StartScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.PipelineVersion = "backlink-v1" // Default
	}

	if req.PipelineVersion == "" {
		req.PipelineVersion = "backlink-v1"
	}

	// Verify it has WAT paths
	hasPaths, err := h.ccRepo.HasWATPaths(r.Context(), crawlID)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "DB_ERROR", "Failed to check crawl paths")
		return
	}
	if !hasPaths {
		sendError(w, http.StatusBadRequest, "INVALID_CRAWL", "Crawl has no WAT paths synchronized")
		return
	}

	if err := h.jobRepo.CreateCommonCrawlJob(r.Context(), crawlID, req.PipelineVersion); err != nil {
		sendError(w, http.StatusConflict, "CONFLICT", err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) PauseJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if err := h.jobRepo.UpdateJobStatus(r.Context(), jobID, "PAUSED"); err != nil {
		sendError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) ResumeJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if err := h.jobRepo.UpdateJobStatus(r.Context(), jobID, "RUNNING"); err != nil {
		sendError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	if err := h.jobRepo.UpdateJobStatus(r.Context(), jobID, "CANCELLED"); err != nil {
		sendError(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func sendError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
}
