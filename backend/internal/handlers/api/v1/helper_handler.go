package v1

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
)

// HelperHandler handles User API requests for metadata and helper endpoints
type HelperHandler struct {
	hashTypeRepo   *repository.HashTypeRepository
	workflowRepo   repository.JobWorkflowRepository
	presetJobRepo  repository.PresetJobRepository
}

// NewHelperHandler creates a new helper handler
func NewHelperHandler(
	hashTypeRepo *repository.HashTypeRepository,
	workflowRepo repository.JobWorkflowRepository,
	presetJobRepo repository.PresetJobRepository,
) *HelperHandler {
	return &HelperHandler{
		hashTypeRepo:  hashTypeRepo,
		workflowRepo:  workflowRepo,
		presetJobRepo: presetJobRepo,
	}
}

// ListHashTypes returns all available hash types
// GET /api/v1/hash-types?enabled_only=true
func (h *HelperHandler) ListHashTypes(w http.ResponseWriter, r *http.Request) {
	// Check if only enabled hash types should be returned
	enabledOnly := r.URL.Query().Get("enabled_only") == "true"

	hashTypes, err := h.hashTypeRepo.List(r.Context(), enabledOnly)
	if err != nil {
		sendAPIError(w, "Failed to retrieve hash types", "HASH_TYPES_RETRIEVAL_FAILED", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hash_types": hashTypes,
		"total":      len(hashTypes),
	})
}

// ListWorkflows returns all available job workflows
// GET /api/v1/workflows
func (h *HelperHandler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows, err := h.workflowRepo.ListWorkflows(r.Context())
	if err != nil {
		sendAPIError(w, "Failed to retrieve workflows", "WORKFLOWS_RETRIEVAL_FAILED", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workflows": workflows,
		"total":     len(workflows),
	})
}

// ListPresetJobs returns all available preset jobs
// GET /api/v1/preset-jobs
func (h *HelperHandler) ListPresetJobs(w http.ResponseWriter, r *http.Request) {
	presetJobs, err := h.presetJobRepo.List(r.Context())
	if err != nil {
		sendAPIError(w, "Failed to retrieve preset jobs", "PRESET_JOBS_RETRIEVAL_FAILED", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"preset_jobs": presetJobs,
		"total":       len(presetJobs),
	})
}
