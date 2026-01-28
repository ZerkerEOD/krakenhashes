package user

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// WebhookHandler handles user webhook management requests
type WebhookHandler struct {
	webhookRepo    *repository.UserWebhookRepository
	webhookService *services.NotificationWebhookService
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(dbConn *sql.DB, webhookService *services.NotificationWebhookService) *WebhookHandler {
	database := &db.DB{DB: dbConn}
	return &WebhookHandler{
		webhookRepo:    repository.NewUserWebhookRepository(database),
		webhookService: webhookService,
	}
}

// GetWebhooks returns all webhooks for the current user
// GET /api/user/webhooks
func (h *WebhookHandler) GetWebhooks(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	webhooks, err := h.webhookRepo.GetByUserID(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to get webhooks: %v", err)
		http.Error(w, "Failed to get webhooks", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"webhooks": webhooks,
	})
}

// CreateWebhook creates a new webhook
// POST /api/user/webhooks
func (h *WebhookHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var request struct {
		Name              string            `json:"name"`
		URL               string            `json:"url"`
		Secret            *string           `json:"secret,omitempty"`
		NotificationTypes []string          `json:"notification_types,omitempty"`
		CustomHeaders     map[string]string `json:"custom_headers,omitempty"`
		RetryCount        *int              `json:"retry_count,omitempty"`
		TimeoutSeconds    *int              `json:"timeout_seconds,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if request.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if request.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Create webhook
	webhook := models.NewUserWebhook(userID, request.Name, request.URL)
	webhook.Secret = request.Secret

	if len(request.NotificationTypes) > 0 {
		webhook.NotificationTypes = request.NotificationTypes
	}

	if request.CustomHeaders != nil {
		webhook.CustomHeaders = make(models.JSONMap)
		for k, v := range request.CustomHeaders {
			webhook.CustomHeaders[k] = v
		}
	}

	if request.RetryCount != nil && *request.RetryCount >= 0 && *request.RetryCount <= 10 {
		webhook.RetryCount = *request.RetryCount
	}

	if request.TimeoutSeconds != nil && *request.TimeoutSeconds > 0 && *request.TimeoutSeconds <= 60 {
		webhook.TimeoutSeconds = *request.TimeoutSeconds
	}

	if err := h.webhookRepo.Create(r.Context(), webhook); err != nil {
		debug.Error("Failed to create webhook: %v", err)
		http.Error(w, "Failed to create webhook", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(webhook)
}

// GetWebhook returns a specific webhook
// GET /api/user/webhooks/{id}
func (h *WebhookHandler) GetWebhook(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	webhookID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid webhook ID", http.StatusBadRequest)
		return
	}

	webhook, err := h.webhookRepo.GetByID(r.Context(), webhookID)
	if err != nil {
		debug.Error("Failed to get webhook: %v", err)
		http.Error(w, "Failed to get webhook", http.StatusInternalServerError)
		return
	}

	if webhook == nil || webhook.UserID != userID {
		http.Error(w, "Webhook not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(webhook)
}

// UpdateWebhook updates a webhook
// PUT /api/user/webhooks/{id}
func (h *WebhookHandler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	webhookID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid webhook ID", http.StatusBadRequest)
		return
	}

	// Get existing webhook
	webhook, err := h.webhookRepo.GetByID(r.Context(), webhookID)
	if err != nil {
		debug.Error("Failed to get webhook: %v", err)
		http.Error(w, "Failed to get webhook", http.StatusInternalServerError)
		return
	}

	if webhook == nil || webhook.UserID != userID {
		http.Error(w, "Webhook not found", http.StatusNotFound)
		return
	}

	var request struct {
		Name              *string           `json:"name,omitempty"`
		URL               *string           `json:"url,omitempty"`
		Secret            *string           `json:"secret,omitempty"`
		IsActive          *bool             `json:"is_active,omitempty"`
		NotificationTypes []string          `json:"notification_types,omitempty"`
		CustomHeaders     map[string]string `json:"custom_headers,omitempty"`
		RetryCount        *int              `json:"retry_count,omitempty"`
		TimeoutSeconds    *int              `json:"timeout_seconds,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update fields
	if request.Name != nil && *request.Name != "" {
		webhook.Name = *request.Name
	}
	if request.URL != nil && *request.URL != "" {
		webhook.URL = *request.URL
	}
	if request.Secret != nil {
		webhook.Secret = request.Secret
	}
	if request.IsActive != nil {
		webhook.IsActive = *request.IsActive
	}
	if request.NotificationTypes != nil {
		webhook.NotificationTypes = request.NotificationTypes
	}
	if request.CustomHeaders != nil {
		webhook.CustomHeaders = make(models.JSONMap)
		for k, v := range request.CustomHeaders {
			webhook.CustomHeaders[k] = v
		}
	}
	if request.RetryCount != nil && *request.RetryCount >= 0 && *request.RetryCount <= 10 {
		webhook.RetryCount = *request.RetryCount
	}
	if request.TimeoutSeconds != nil && *request.TimeoutSeconds > 0 && *request.TimeoutSeconds <= 60 {
		webhook.TimeoutSeconds = *request.TimeoutSeconds
	}

	if err := h.webhookRepo.Update(r.Context(), webhook); err != nil {
		debug.Error("Failed to update webhook: %v", err)
		http.Error(w, "Failed to update webhook", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(webhook)
}

// DeleteWebhook deletes a webhook
// DELETE /api/user/webhooks/{id}
func (h *WebhookHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	webhookID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid webhook ID", http.StatusBadRequest)
		return
	}

	if err := h.webhookRepo.Delete(r.Context(), webhookID, userID); err != nil {
		debug.Error("Failed to delete webhook: %v", err)
		http.Error(w, "Failed to delete webhook", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
	})
}

// TestWebhook sends a test notification to a webhook
// POST /api/user/webhooks/{id}/test
func (h *WebhookHandler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	webhookID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid webhook ID", http.StatusBadRequest)
		return
	}

	// Get webhook
	webhook, err := h.webhookRepo.GetByID(r.Context(), webhookID)
	if err != nil {
		debug.Error("Failed to get webhook: %v", err)
		http.Error(w, "Failed to get webhook", http.StatusInternalServerError)
		return
	}

	if webhook == nil || webhook.UserID != userID {
		http.Error(w, "Webhook not found", http.StatusNotFound)
		return
	}

	// Send test
	err = h.webhookService.TestWebhook(r.Context(), webhook.URL, webhook.Secret, webhook.CustomHeaders)
	if err != nil {
		debug.Warning("Webhook test failed: %v", err)
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

// TestWebhookURL sends a test to a URL without saving
// POST /api/user/webhooks/test-url
func (h *WebhookHandler) TestWebhookURL(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL    string  `json:"url"`
		Secret *string `json:"secret,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if request.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	err := h.webhookService.TestWebhook(r.Context(), request.URL, request.Secret, nil)
	if err != nil {
		debug.Warning("Webhook test failed: %v", err)
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
