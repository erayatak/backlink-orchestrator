package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/backlink-orchestrator/internal/jobs"
)

type AdminHandler struct {
	jobRepo *jobs.Repository
}

func NewAdminHandler(jobRepo *jobs.Repository) *AdminHandler {
	return &AdminHandler{jobRepo: jobRepo}
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
