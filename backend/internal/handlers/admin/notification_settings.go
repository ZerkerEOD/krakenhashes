package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// NotificationSettingsHandler handles admin notification settings
type NotificationSettingsHandler struct {
	systemSettingsRepo *repository.SystemSettingsRepository
	webhookRepo        *repository.UserWebhookRepository
	webhookService     *services.NotificationWebhookService
}

// NewNotificationSettingsHandler creates a new notification settings handler
func NewNotificationSettingsHandler(dbConn *sql.DB, webhookService *services.NotificationWebhookService) *NotificationSettingsHandler {
	database := &db.DB{DB: dbConn}
	return &NotificationSettingsHandler{
		systemSettingsRepo: repository.NewSystemSettingsRepository(database),
		webhookRepo:        repository.NewUserWebhookRepository(database),
		webhookService:     webhookService,
	}
}

// GlobalWebhookSettingsResponse represents the response for global webhook settings
type GlobalWebhookSettingsResponse struct {
	URL           string `json:"url"`
	Enabled       bool   `json:"enabled"`
	HasSecret     bool   `json:"has_secret"`
	CustomHeaders string `json:"custom_headers,omitempty"`
}

// GetGlobalWebhookSettings returns the global webhook configuration
// GET /api/admin/notification-settings
func (h *NotificationSettingsHandler) GetGlobalWebhookSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all related settings
	urlSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "global_webhook_url")
	enabledSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "global_webhook_enabled")
	secretSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "global_webhook_secret")
	headersSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "global_webhook_custom_headers")

	response := GlobalWebhookSettingsResponse{
		URL:       "",
		Enabled:   false,
		HasSecret: false,
	}

	if urlSetting.Value != nil {
		response.URL = *urlSetting.Value
	}
	if enabledSetting.Value != nil && *enabledSetting.Value == "true" {
		response.Enabled = true
	}
	if secretSetting.Value != nil && *secretSetting.Value != "" {
		response.HasSecret = true
	}
	if headersSetting.Value != nil {
		response.CustomHeaders = *headersSetting.Value
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateGlobalWebhookSettings updates the global webhook configuration
// PUT /api/admin/notification-settings
func (h *NotificationSettingsHandler) UpdateGlobalWebhookSettings(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL           *string `json:"url,omitempty"`
		Enabled       *bool   `json:"enabled,omitempty"`
		Secret        *string `json:"secret,omitempty"`
		CustomHeaders *string `json:"custom_headers,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Update settings
	if request.URL != nil {
		if err := h.systemSettingsRepo.UpdateSetting(ctx, "global_webhook_url", *request.URL); err != nil {
			debug.Error("Failed to update global_webhook_url: %v", err)
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	if request.Enabled != nil {
		value := "false"
		if *request.Enabled {
			value = "true"
		}
		if err := h.systemSettingsRepo.UpdateSetting(ctx, "global_webhook_enabled", value); err != nil {
			debug.Error("Failed to update global_webhook_enabled: %v", err)
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	if request.Secret != nil {
		if err := h.systemSettingsRepo.UpdateSetting(ctx, "global_webhook_secret", *request.Secret); err != nil {
			debug.Error("Failed to update global_webhook_secret: %v", err)
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	if request.CustomHeaders != nil {
		if err := h.systemSettingsRepo.UpdateSetting(ctx, "global_webhook_custom_headers", *request.CustomHeaders); err != nil {
			debug.Error("Failed to update global_webhook_custom_headers: %v", err)
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	// Return updated settings
	h.GetGlobalWebhookSettings(w, r)
}

// TestGlobalWebhook sends a test to the global webhook
// POST /api/admin/notification-settings/test-webhook
func (h *NotificationSettingsHandler) TestGlobalWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get current settings
	urlSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "global_webhook_url")
	secretSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "global_webhook_secret")
	headersSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "global_webhook_custom_headers")

	if urlSetting.Value == nil || *urlSetting.Value == "" {
		http.Error(w, "No webhook URL configured", http.StatusBadRequest)
		return
	}

	var secret *string
	if secretSetting.Value != nil && *secretSetting.Value != "" {
		secret = secretSetting.Value
	}

	var customHeaders models.JSONMap
	if headersSetting.Value != nil && *headersSetting.Value != "" {
		if err := json.Unmarshal([]byte(*headersSetting.Value), &customHeaders); err != nil {
			debug.Warning("Failed to parse custom headers: %v", err)
		}
	}

	err := h.webhookService.TestWebhook(ctx, *urlSetting.Value, secret, customHeaders)
	if err != nil {
		debug.Warning("Global webhook test failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Test notification sent successfully",
	})
}

// GetAllUserWebhooks returns all user webhooks (admin view)
// GET /api/admin/users/webhooks
func (h *NotificationSettingsHandler) GetAllUserWebhooks(w http.ResponseWriter, r *http.Request) {
	webhooks, err := h.webhookRepo.GetAllWebhooksAdmin(r.Context())
	if err != nil {
		debug.Error("Failed to get all user webhooks: %v", err)
		http.Error(w, "Failed to get webhooks", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"webhooks": webhooks,
	})
}

// GetAgentOfflineSettings returns agent offline notification settings
// GET /api/admin/notification-settings/agent-offline
func (h *NotificationSettingsHandler) GetAgentOfflineSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	bufferSetting, _ := h.systemSettingsRepo.GetSetting(ctx, "agent_offline_buffer_minutes")

	bufferMinutes := 10 // Default
	if bufferSetting.Value != nil {
		var minutes int
		if err := json.Unmarshal([]byte(*bufferSetting.Value), &minutes); err == nil && minutes > 0 {
			bufferMinutes = minutes
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"buffer_minutes": bufferMinutes,
	})
}

// UpdateAgentOfflineSettings updates agent offline notification settings
// PUT /api/admin/notification-settings/agent-offline
func (h *NotificationSettingsHandler) UpdateAgentOfflineSettings(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BufferMinutes *int `json:"buffer_minutes,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	if request.BufferMinutes != nil && *request.BufferMinutes > 0 {
		value := json.Number(*request.BufferMinutes).String()
		if err := h.systemSettingsRepo.UpdateSetting(ctx, "agent_offline_buffer_minutes", value); err != nil {
			debug.Error("Failed to update agent_offline_buffer_minutes: %v", err)
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	h.GetAgentOfflineSettings(w, r)
}
