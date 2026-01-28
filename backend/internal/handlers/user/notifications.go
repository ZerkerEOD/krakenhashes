package user

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// NotificationHandler handles notification-related requests
type NotificationHandler struct {
	dispatcher *services.NotificationDispatcher
	hub        *services.UserNotificationHub
}

// NewNotificationHandler creates a new notification handler
func NewNotificationHandler(
	dispatcher *services.NotificationDispatcher,
	hub *services.UserNotificationHub,
) *NotificationHandler {
	return &NotificationHandler{
		dispatcher: dispatcher,
		hub:        hub,
	}
}

// GetNotifications returns a list of notifications for the current user
// GET /api/user/notifications
func (h *NotificationHandler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	params := models.NotificationListParams{
		UserID: userID,
		Limit:  20,
		Offset: 0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			params.Offset = offset
		}
	}

	if category := r.URL.Query().Get("category"); category != "" {
		params.Category = category
	}

	if notificationType := r.URL.Query().Get("type"); notificationType != "" {
		params.Type = models.NotificationType(notificationType)
	}

	if readStr := r.URL.Query().Get("read"); readStr != "" {
		read := readStr == "true"
		params.ReadOnly = &read
	}

	// Get notifications
	result, err := h.dispatcher.GetUserNotifications(r.Context(), params)
	if err != nil {
		debug.Error("Failed to get notifications: %v", err)
		http.Error(w, "Failed to get notifications", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GetRecentNotifications returns the most recent notifications for the current user
// GET /api/user/notifications/recent
func (h *NotificationHandler) GetRecentNotifications(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	limit := 5
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 20 {
			limit = l
		}
	}

	notifications, err := h.dispatcher.GetRecentNotifications(r.Context(), userID, limit)
	if err != nil {
		debug.Error("Failed to get recent notifications: %v", err)
		http.Error(w, "Failed to get notifications", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"notifications": notifications,
	})
}

// GetUnreadCount returns the number of unread notifications
// GET /api/user/notifications/unread-count
func (h *NotificationHandler) GetUnreadCount(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	count, err := h.dispatcher.GetUnreadCount(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to get unread count: %v", err)
		http.Error(w, "Failed to get unread count", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"count": count,
	})
}

// MarkAsRead marks a notification as read
// PUT /api/user/notifications/{id}/read
func (h *NotificationHandler) MarkAsRead(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	notificationID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid notification ID", http.StatusBadRequest)
		return
	}

	if err := h.dispatcher.MarkAsRead(r.Context(), notificationID, userID); err != nil {
		debug.Error("Failed to mark notification as read: %v", err)
		http.Error(w, "Failed to mark as read", http.StatusInternalServerError)
		return
	}

	// Notify connected clients about the read status
	if h.hub != nil {
		h.hub.SendMarkRead(userID, []uuid.UUID{notificationID})

		// Update unread count
		count, _ := h.dispatcher.GetUnreadCount(r.Context(), userID)
		h.hub.SendUnreadCount(userID, count)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
	})
}

// MarkAllAsRead marks all notifications as read
// PUT /api/user/notifications/read-all
func (h *NotificationHandler) MarkAllAsRead(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	count, err := h.dispatcher.MarkAllAsRead(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to mark all as read: %v", err)
		http.Error(w, "Failed to mark all as read", http.StatusInternalServerError)
		return
	}

	// Notify connected clients about updated count
	if h.hub != nil {
		h.hub.SendUnreadCount(userID, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "success",
		"marked_count": count,
	})
}

// DeleteNotifications deletes notifications
// DELETE /api/user/notifications
func (h *NotificationHandler) DeleteNotifications(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserIDFromContext(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var request struct {
		IDs []uuid.UUID `json:"ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(request.IDs) == 0 {
		http.Error(w, "No notification IDs provided", http.StatusBadRequest)
		return
	}

	deleted, err := h.dispatcher.DeleteNotifications(r.Context(), request.IDs, userID)
	if err != nil {
		debug.Error("Failed to delete notifications: %v", err)
		http.Error(w, "Failed to delete notifications", http.StatusInternalServerError)
		return
	}

	// Update unread count
	if h.hub != nil {
		count, _ := h.dispatcher.GetUnreadCount(r.Context(), userID)
		h.hub.SendUnreadCount(userID, count)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"deleted_count": deleted,
	})
}

// getUserIDFromContext extracts the user ID from the request context
func getUserIDFromContext(r *http.Request) (uuid.UUID, error) {
	userIDValue := r.Context().Value("user_id")
	if userIDValue == nil {
		return uuid.Nil, http.ErrNoCookie
	}

	switch v := userIDValue.(type) {
	case uuid.UUID:
		return v, nil
	case string:
		return uuid.Parse(v)
	default:
		return uuid.Nil, http.ErrNoCookie
	}
}
