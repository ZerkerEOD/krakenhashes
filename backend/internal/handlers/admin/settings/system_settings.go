package settings

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"errors"
)

// SystemSettingsHandler handles system settings requests
type SystemSettingsHandler struct {
	systemSettingsRepo *repository.SystemSettingsRepository
	presetJobRepo      repository.PresetJobRepository
}

// NewSystemSettingsHandler creates a new system settings handler
func NewSystemSettingsHandler(
	systemSettingsRepo *repository.SystemSettingsRepository,
	presetJobRepo repository.PresetJobRepository,
) *SystemSettingsHandler {
	return &SystemSettingsHandler{
		systemSettingsRepo: systemSettingsRepo,
		presetJobRepo:      presetJobRepo,
	}
}

// GetMaxPriority retrieves the current maximum priority setting
func (h *SystemSettingsHandler) GetMaxPriority(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting max priority setting")

	maxPriority, err := h.systemSettingsRepo.GetMaxJobPriority(r.Context())
	if err != nil {
		debug.Error("Failed to get max priority: %v", err)
		http.Error(w, "Failed to get max priority setting", http.StatusInternalServerError)
		return
	}

	response := models.MaxPriorityConfig{
		MaxPriority: maxPriority,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateMaxPriority updates the maximum priority setting with validation
func (h *SystemSettingsHandler) UpdateMaxPriority(w http.ResponseWriter, r *http.Request) {
	debug.Info("Received request to update max priority setting")

	var request struct {
		MaxPriority int `json:"max_priority"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		debug.Error("Failed to decode max priority request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	debug.Info("Decoded max priority: %d", request.MaxPriority)

	// Validate the new max priority value
	if request.MaxPriority < 1 {
		http.Error(w, "Maximum priority must be at least 1", http.StatusBadRequest)
		return
	}

	if request.MaxPriority > 1000000 {
		http.Error(w, "Maximum priority cannot exceed 1,000,000", http.StatusBadRequest)
		return
	}

	// Check if any existing preset jobs have priority higher than the new max
	existingJobs, err := h.presetJobRepo.List(r.Context())
	if err != nil {
		debug.Error("Failed to list preset jobs for validation: %v", err)
		http.Error(w, "Failed to validate existing jobs", http.StatusInternalServerError)
		return
	}

	var conflictingJobs []string
	for _, job := range existingJobs {
		if job.Priority > request.MaxPriority {
			conflictingJobs = append(conflictingJobs, fmt.Sprintf("'%s' (priority: %d)", job.Name, job.Priority))
		}
	}

	if len(conflictingJobs) > 0 {
		errorMsg := fmt.Sprintf("Cannot set maximum priority to %d. The following preset jobs have higher priorities: %v",
			request.MaxPriority, conflictingJobs)
		debug.Warning("Max priority validation failed: %s", errorMsg)

		response := map[string]interface{}{
			"error":            "Validation failed",
			"message":          errorMsg,
			"conflicting_jobs": conflictingJobs,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(response)
		return
	}

	// TODO: Add validation for active/queued jobs once job queue system is implemented
	// For now, we only validate preset jobs

	// Update the setting
	err = h.systemSettingsRepo.SetMaxJobPriority(r.Context(), request.MaxPriority)
	if err != nil {
		debug.Error("Failed to update max priority: %v", err)
		http.Error(w, "Failed to update max priority setting", http.StatusInternalServerError)
		return
	}

	debug.Info("Successfully updated max priority to %d", request.MaxPriority)

	// Return the updated setting
	response := models.MaxPriorityConfig{
		MaxPriority: request.MaxPriority,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetMaxPriorityForUsers retrieves the current maximum priority setting for non-admin users
func (h *SystemSettingsHandler) GetMaxPriorityForUsers(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting max priority setting for users")

	maxPriority, err := h.systemSettingsRepo.GetMaxJobPriority(r.Context())
	if err != nil {
		debug.Error("Failed to get max priority: %v", err)
		http.Error(w, "Failed to get max priority setting", http.StatusInternalServerError)
		return
	}

	response := models.MaxPriorityConfig{
		MaxPriority: maxPriority,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListSettings retrieves all system settings
func (h *SystemSettingsHandler) ListSettings(w http.ResponseWriter, r *http.Request) {
	debug.Debug("Getting all system settings")

	settings, err := h.systemSettingsRepo.GetAllSettings(r.Context())
	if err != nil {
		debug.Error("Failed to get system settings: %v", err)
		http.Error(w, "Failed to get system settings", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"data": settings,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateSetting updates a specific system setting
// guardedSettingKeys maps a setting key to the endpoint that owns its validation.
//
// Reads are deliberately left open; only writes are redirected.
var guardedSettingKeys = map[string]string{
	"tls_additional_ip_addresses": "PUT /api/admin/tls/sans",
	"tls_additional_dns_names":    "PUT /api/admin/tls/sans",
}

func (h *SystemSettingsHandler) UpdateSetting(w http.ResponseWriter, r *http.Request) {
	debug.Info("Received request to update system setting")

	var request struct {
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		debug.Error("Failed to decode update setting request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Extract setting key from URL path
	settingKey := r.URL.Path[len("/api/admin/settings/"):]

	// Some settings carry invariants this generic key/value endpoint cannot
	// enforce. Writing them here would be a complete end-run around the
	// validation on their dedicated endpoint -- the certificate name lists, for
	// example, would let an administrator put a public, internet-routable
	// address into the server certificate of a tool that must never be exposed.
	if endpoint, guarded := guardedSettingKeys[settingKey]; guarded {
		debug.Warning("Refused a generic write to guarded setting %s", settingKey)
		http.Error(w, "This setting is validated elsewhere and cannot be written here. Use "+endpoint+".",
			http.StatusBadRequest)
		return
	}

	debug.Info("Updating setting %s to value: %s", settingKey, request.Value)

	// Update the setting
	err := h.systemSettingsRepo.UpdateSetting(r.Context(), settingKey, request.Value)
	if err != nil {
		debug.Error("Failed to update setting %s: %v", settingKey, err)
		http.Error(w, "Failed to update setting", http.StatusInternalServerError)
		return
	}

	debug.Info("Successfully updated setting %s", settingKey)

	// Return the updated setting
	setting, err := h.systemSettingsRepo.GetSetting(r.Context(), settingKey)
	if err != nil {
		debug.Error("Failed to get updated setting: %v", err)
		http.Error(w, "Failed to retrieve updated setting", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(setting)
}

// GetSetting retrieves a specific system setting by key
func (h *SystemSettingsHandler) GetSetting(w http.ResponseWriter, r *http.Request) {
	// Extract setting key from URL path
	settingKey := r.URL.Path[len("/api/admin/settings/"):]
	
	debug.Debug("Getting system setting: %s", settingKey)

	setting, err := h.systemSettingsRepo.GetSetting(r.Context(), settingKey)
	if err != nil {
		debug.Error("Failed to get setting %s: %v", settingKey, err)
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Setting not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get setting", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(setting)
}
