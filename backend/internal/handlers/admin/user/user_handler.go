package user

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/password"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// UserHandler handles API requests for admin user management
type UserHandler struct {
	userRepo *repository.UserRepository
	db       *db.DB
}

// NewUserHandler creates a new handler instance
func NewUserHandler(ur *repository.UserRepository, database *db.DB) *UserHandler {
	return &UserHandler{
		userRepo: ur,
		db:       database,
	}
}

// CreateUser godoc
// @Summary Create a new user
// @Description Creates a new user account with the specified details
// @Tags Admin Users
// @Accept json
// @Produce json
// @Param user body object{username=string,email=string,password=string,role=string} true "User creation data"
// @Success 201 {object} httputil.SuccessResponse{data=object{message=string,user_id=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 409 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users [post]
// @Security ApiKeyAuth
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var createData struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}

	if err := json.NewDecoder(r.Body).Decode(&createData); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if createData.Username == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Username is required")
		return
	}
	if createData.Email == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Email is required")
		return
	}
	if createData.Password == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Password is required")
		return
	}
	if createData.Role == "" {
		createData.Role = "user" // Default to user role if not specified
	}

	// Validate email format
	if !strings.Contains(createData.Email, "@") {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid email format")
		return
	}

	// Validate role
	if createData.Role != "admin" && createData.Role != "user" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid role. Must be 'admin' or 'user'")
		return
	}

	// Get password policy from auth settings
	authSettings, err := h.db.GetAuthSettings()
	if err != nil {
		debug.Error("Failed to get auth settings: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve password policy")
		return
	}

	// Validate password against policy
	passwordErrors := []string{}
	
	if len(createData.Password) < authSettings.MinPasswordLength {
		passwordErrors = append(passwordErrors, fmt.Sprintf("Password must be at least %d characters long", authSettings.MinPasswordLength))
	}
	
	if authSettings.RequireUppercase && !strings.ContainsAny(createData.Password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		passwordErrors = append(passwordErrors, "Password must contain at least one uppercase letter")
	}
	
	if authSettings.RequireLowercase && !strings.ContainsAny(createData.Password, "abcdefghijklmnopqrstuvwxyz") {
		passwordErrors = append(passwordErrors, "Password must contain at least one lowercase letter")
	}
	
	if authSettings.RequireNumbers && !strings.ContainsAny(createData.Password, "0123456789") {
		passwordErrors = append(passwordErrors, "Password must contain at least one number")
	}
	
	if authSettings.RequireSpecialChars && !strings.ContainsAny(createData.Password, "!@#$%^&*(),.?\":{}|<>") {
		passwordErrors = append(passwordErrors, "Password must contain at least one special character")
	}
	
	if len(passwordErrors) > 0 {
		httputil.RespondWithError(w, http.StatusBadRequest, strings.Join(passwordErrors, "; "))
		return
	}

	// Check if username or email already exists
	existingUser, _ := h.userRepo.GetByUsername(r.Context(), createData.Username)
	if existingUser != nil {
		httputil.RespondWithError(w, http.StatusConflict, "Username already exists")
		return
	}

	existingUser, _ = h.userRepo.GetByEmail(r.Context(), createData.Email)
	if existingUser != nil {
		httputil.RespondWithError(w, http.StatusConflict, "Email already exists")
		return
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(createData.Password), bcrypt.DefaultCost)
	if err != nil {
		debug.Error("Failed to hash password: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to process password")
		return
	}

	// Create new user object
	now := time.Now()
	newUser := &models.User{
		ID:                 uuid.New(),
		Username:           createData.Username,
		Email:              createData.Email,
		PasswordHash:       string(hashedPassword),
		Role:               createData.Role,
		CreatedAt:          now,
		UpdatedAt:          now,
		AccountEnabled:     true,
		LastPasswordChange: now,
		MFAEnabled:         false,
		MFAType:            []string{},
		BackupCodes:        []string{},
	}

	// Create user in database
	err = h.userRepo.Create(r.Context(), newUser)
	if err != nil {
		debug.Error("Failed to create user: %v", err)
		if strings.Contains(err.Error(), "duplicate key") {
			httputil.RespondWithError(w, http.StatusConflict, "Username or email already exists")
			return
		}
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// Get admin ID from context for logging
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if ok {
		adminID, err := uuid.Parse(adminIDStr)
		if err == nil {
			debug.Info("Admin %s created new user: %s (username: %s, role: %s)", adminID, newUser.ID, newUser.Username, newUser.Role)
		} else {
			debug.Info("Admin (invalid ID) created new user: %s (username: %s, role: %s)", newUser.ID, newUser.Username, newUser.Role)
		}
	} else {
		debug.Info("Admin (unknown) created new user: %s (username: %s, role: %s)", newUser.ID, newUser.Username, newUser.Role)
	}

	httputil.RespondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"data": map[string]string{
			"message": "User created successfully",
			"user_id": newUser.ID.String(),
		},
	})
}

// ListUsers godoc
// @Summary List all users
// @Description Retrieves a list of all users in the system for admin view
// @Tags Admin Users
// @Produce json
// @Success 200 {object} httputil.SuccessResponse{data=[]models.User}
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users [get]
// @Security ApiKeyAuth
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListAll(r.Context())
	if err != nil {
		debug.Error("Failed to list users: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve users")
		return
	}

	// Clear sensitive data before sending
	for i := range users {
		users[i].PasswordHash = ""
		users[i].MFASecret = ""
		users[i].BackupCodes = nil
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{"data": users})
}

// GetUser godoc
// @Summary Get user details
// @Description Retrieves detailed information about a specific user
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} httputil.SuccessResponse{data=models.User}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id} [get]
// @Security ApiKeyAuth
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.userRepo.GetDetails(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to get user details: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve user details")
		return
	}

	// Clear sensitive data before sending
	user.PasswordHash = ""
	user.MFASecret = ""
	user.BackupCodes = nil

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{"data": user})
}

// UpdateUser godoc
// @Summary Update user details
// @Description Updates username and/or email for a user
// @Tags Admin Users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param user body object{username=string,email=string} true "User update data"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 409 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id} [put]
// @Security ApiKeyAuth
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var updateData struct {
		Username *string `json:"username,omitempty"`
		Email    *string `json:"email,omitempty"`
		Role     *string `json:"role,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate email format if provided
	if updateData.Email != nil && *updateData.Email != "" {
		if !strings.Contains(*updateData.Email, "@") {
			httputil.RespondWithError(w, http.StatusBadRequest, "Invalid email format")
			return
		}
	}

	// Validate role if provided
	if updateData.Role != nil {
		// Prevent setting role to system
		if *updateData.Role == "system" {
			httputil.RespondWithError(w, http.StatusBadRequest, "Cannot assign system role")
			return
		}
		// Validate role is valid
		if *updateData.Role != "admin" && *updateData.Role != "user" {
			httputil.RespondWithError(w, http.StatusBadRequest, "Invalid role. Must be 'admin' or 'user'")
			return
		}
	}

	// Check if target user is system user
	targetUser, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to get user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}

	// Prevent modifying system users
	if targetUser.Role == "system" {
		httputil.RespondWithError(w, http.StatusForbidden, "Cannot modify system users")
		return
	}

	err = h.userRepo.UpdateDetails(r.Context(), userID, updateData.Username, updateData.Email, updateData.Role)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		if strings.Contains(err.Error(), "already exists") {
			httputil.RespondWithError(w, http.StatusConflict, err.Error())
			return
		}
		debug.Error("Failed to update user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	debug.Info("Admin updated user details: %s", userID)
	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]string{"message": "User updated successfully"},
	})
}

// DisableUser godoc
// @Summary Disable user account
// @Description Disables a user account, preventing login
// @Tags Admin Users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param data body object{reason=string} true "Disable reason"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/disable [post]
// @Security ApiKeyAuth
func (h *UserHandler) DisableUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Check if target user is system user
	targetUser, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to get user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}

	// Prevent modifying system users
	if targetUser.Role == "system" {
		httputil.RespondWithError(w, http.StatusForbidden, "Cannot disable system users")
		return
	}

	var disableData struct {
		Reason string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&disableData); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if disableData.Reason == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Reason is required")
		return
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get admin ID")
		return
	}
	
	adminID, err := uuid.Parse(adminIDStr)
	if err != nil {
		debug.Error("Failed to parse admin ID: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Invalid admin ID format")
		return
	}

	err = h.userRepo.DisableAccount(r.Context(), userID, disableData.Reason, adminID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to disable user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to disable user")
		return
	}

	debug.Info("Admin %s disabled user account: %s (reason: %s)", adminID, userID, disableData.Reason)
	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]string{"message": "User account disabled successfully"},
	})
}

// EnableUser godoc
// @Summary Enable user account
// @Description Re-enables a previously disabled user account
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/enable [post]
// @Security ApiKeyAuth
func (h *UserHandler) EnableUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	err = h.userRepo.EnableAccount(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to enable user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to enable user")
		return
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if ok {
		adminID, err := uuid.Parse(adminIDStr)
		if err == nil {
			debug.Info("Admin %s enabled user account: %s", adminID, userID)
		} else {
			debug.Info("Admin (invalid ID) enabled user account: %s", userID)
		}
	} else {
		debug.Info("Admin (unknown) enabled user account: %s", userID)
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]string{"message": "User account enabled successfully"},
	})
}

// ResetUserPassword godoc
// @Summary Reset user password
// @Description Resets a user's password to a new value
// @Tags Admin Users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param data body object{password=string,temporary=boolean} true "New password"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string,temporary_password=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/reset-password [post]
// @Security ApiKeyAuth
func (h *UserHandler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Check if target user is system user
	targetUser, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to get user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}

	// Prevent modifying system users
	if targetUser.Role == "system" {
		httputil.RespondWithError(w, http.StatusForbidden, "Cannot reset password for system users")
		return
	}

	var resetData struct {
		Password  string `json:"password"`
		Temporary bool   `json:"temporary"`
	}

	if err := json.NewDecoder(r.Body).Decode(&resetData); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Generate temporary password if not provided
	if resetData.Password == "" && resetData.Temporary {
		resetData.Password = password.GenerateTemporaryPassword()
	}

	if resetData.Password == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Password is required")
		return
	}

	// Validate password strength if not temporary
	if !resetData.Temporary {
		// TODO: Use password validator from auth settings
		if len(resetData.Password) < 8 {
			httputil.RespondWithError(w, http.StatusBadRequest, "Password must be at least 8 characters long")
			return
		}
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(resetData.Password), bcrypt.DefaultCost)
	if err != nil {
		debug.Error("Failed to hash password: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to process password")
		return
	}

	err = h.userRepo.ResetPassword(r.Context(), userID, string(hashedPassword))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to reset password: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to reset password")
		return
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if ok {
		adminID, err := uuid.Parse(adminIDStr)
		if err == nil {
			debug.Info("Admin %s reset password for user: %s", adminID, userID)
		} else {
			debug.Info("Admin (invalid ID) reset password for user: %s", userID)
		}
	} else {
		debug.Info("Admin (unknown) reset password for user: %s", userID)
	}

	response := map[string]interface{}{
		"data": map[string]string{
			"message": "Password reset successfully",
		},
	}

	// Include temporary password in response if generated
	if resetData.Temporary {
		response["data"].(map[string]string)["temporary_password"] = resetData.Password
	}

	httputil.RespondWithJSON(w, http.StatusOK, response)
}

// DisableUserMFA godoc
// @Summary Disable user MFA
// @Description Disables multi-factor authentication for a user
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/disable-mfa [post]
// @Security ApiKeyAuth
func (h *UserHandler) DisableUserMFA(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	err = h.userRepo.DisableMFA(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to disable MFA: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to disable MFA")
		return
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if ok {
		adminID, err := uuid.Parse(adminIDStr)
		if err == nil {
			debug.Info("Admin %s disabled MFA for user: %s", adminID, userID)
		} else {
			debug.Info("Admin (invalid ID) disabled MFA for user: %s", userID)
		}
	} else {
		debug.Info("Admin (unknown) disabled MFA for user: %s", userID)
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]string{"message": "MFA disabled successfully"},
	})
}

// UnlockUser godoc
// @Summary Unlock user account
// @Description Unlocks a user account that was locked due to failed login attempts
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/unlock [post]
// @Security ApiKeyAuth
func (h *UserHandler) UnlockUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	err = h.userRepo.UnlockAccount(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to unlock user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to unlock user")
		return
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if ok {
		adminID, err := uuid.Parse(adminIDStr)
		if err == nil {
			debug.Info("Admin %s unlocked user account: %s", adminID, userID)
		} else {
			debug.Info("Admin (invalid ID) unlocked user account: %s", userID)
		}
	} else {
		debug.Info("Admin (unknown) unlocked user account: %s", userID)
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]string{"message": "User account unlocked successfully"},
	})
}

// DeleteUser godoc
// @Summary Delete user account
// @Description Soft deletes a user account (sets deleted_at timestamp)
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 403 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id} [delete]
// @Security ApiKeyAuth
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if !ok {
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get admin ID")
		return
	}

	adminID, err := uuid.Parse(adminIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusInternalServerError, "Invalid admin ID format")
		return
	}

	// Prevent self-deletion
	if userID == adminID {
		httputil.RespondWithError(w, http.StatusForbidden, "Cannot delete your own account")
		return
	}

	// Check if target user is system user
	targetUser, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found")
			return
		}
		debug.Error("Failed to get user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}

	// Prevent deleting system users
	if targetUser.Role == "system" {
		httputil.RespondWithError(w, http.StatusForbidden, "Cannot delete system users")
		return
	}

	// Perform soft delete
	err = h.userRepo.SoftDelete(r.Context(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "already deleted") {
			httputil.RespondWithError(w, http.StatusNotFound, "User not found or already deleted")
			return
		}
		debug.Error("Failed to delete user: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	debug.Info("Admin %s deleted user account: %s (username: %s)", adminID, userID, targetUser.Username)
	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]string{"message": "User deleted successfully"},
	})
}

// GetUserLoginAttempts godoc
// @Summary Get user login attempts
// @Description Retrieves login attempt history for a specific user
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Param limit query int false "Number of attempts to retrieve (default: 50, max: 100)"
// @Success 200 {object} httputil.SuccessResponse{data=[]models.LoginAttempt}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/login-attempts [get]
// @Security ApiKeyAuth
func (h *UserHandler) GetUserLoginAttempts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Parse limit from query params (default 50, max 100)
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := fmt.Sscanf(limitStr, "%d", &limit); err == nil && parsedLimit == 1 {
			if limit > 100 {
				limit = 100
			} else if limit < 1 {
				limit = 50
			}
		}
	}

	attempts, err := h.db.GetUserLoginAttempts(userID, limit)
	if err != nil {
		debug.Error("Failed to get login attempts for user %s: %v", userID, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve login attempts")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": attempts,
	})
}

// GetUserSessions godoc
// @Summary Get user active sessions
// @Description Retrieves all active sessions for a specific user
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} httputil.SuccessResponse{data=[]models.ActiveSession}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/sessions [get]
// @Security ApiKeyAuth
func (h *UserHandler) GetUserSessions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	sessions, err := h.db.GetUserSessions(userID)
	if err != nil {
		debug.Error("Failed to get sessions for user %s: %v", userID, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve sessions")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": sessions,
	})
}

// TerminateSession godoc
// @Summary Terminate a specific user session
// @Description Terminates a specific active session for a user
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Param sessionId path string true "Session ID"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 403 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/sessions/{sessionId} [delete]
// @Security ApiKeyAuth
func (h *UserHandler) TerminateSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]
	sessionIDStr := vars["sessionId"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid session ID")
		return
	}

	// Verify session belongs to user and get token_id (security check)
	sessions, err := h.db.GetUserSessions(userID)
	if err != nil {
		debug.Error("Failed to get sessions for user %s: %v", userID, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to verify session")
		return
	}

	var tokenID *uuid.UUID
	sessionBelongsToUser := false
	for _, session := range sessions {
		if session.ID == sessionID {
			sessionBelongsToUser = true
			tokenID = session.TokenID
			break
		}
	}

	if !sessionBelongsToUser {
		httputil.RespondWithError(w, http.StatusForbidden, "Session does not belong to user")
		return
	}

	// Delete the token (which will cascade delete the session)
	if tokenID != nil {
		err = h.db.RemoveToken(*tokenID)
		if err != nil {
			debug.Error("Failed to delete token for session %s: %v", sessionID, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to terminate session")
			return
		}
	} else {
		// Fallback for sessions without token_id (backwards compatibility)
		err = h.db.DeleteSession(sessionID)
		if err != nil {
			debug.Error("Failed to terminate session %s: %v", sessionID, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to terminate session")
			return
		}
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if ok {
		adminID, err := uuid.Parse(adminIDStr)
		if err == nil {
			debug.Info("Admin %s terminated session %s for user %s", adminID, sessionID, userID)
		}
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]string{"message": "Session terminated successfully"},
	})
}

// TerminateAllUserSessions godoc
// @Summary Terminate all user sessions
// @Description Terminates all active sessions for a specific user
// @Tags Admin Users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} httputil.SuccessResponse{data=object{message=string,count=int}}
// @Failure 400 {object} httputil.ErrorResponse
// @Failure 404 {object} httputil.ErrorResponse
// @Failure 500 {object} httputil.ErrorResponse
// @Router /admin/users/{id}/sessions [delete]
// @Security ApiKeyAuth
func (h *UserHandler) TerminateAllUserSessions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Get sessions and extract token IDs
	sessions, err := h.db.GetUserSessions(userID)
	if err != nil {
		debug.Error("Failed to get sessions for user %s: %v", userID, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve sessions")
		return
	}
	count := len(sessions)

	// Delete all tokens (which will cascade delete sessions)
	var deleteErrors []error
	for _, session := range sessions {
		if session.TokenID != nil {
			if err := h.db.RemoveToken(*session.TokenID); err != nil {
				debug.Error("Failed to delete token %s: %v", *session.TokenID, err)
				deleteErrors = append(deleteErrors, err)
			}
		}
	}

	// Also clean up any sessions without token_id (backwards compatibility)
	if err := h.db.DeleteUserSessions(userID); err != nil {
		debug.Error("Failed to delete orphaned sessions for user %s: %v", userID, err)
		// Don't fail the request for this
	}

	// If we had errors deleting tokens, report it
	if len(deleteErrors) > 0 {
		debug.Error("Failed to terminate %d/%d sessions for user %s", len(deleteErrors), count, userID)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to terminate some sessions")
		return
	}

	// Get admin ID from context
	adminIDStr, ok := r.Context().Value("user_id").(string)
	if ok {
		adminID, err := uuid.Parse(adminIDStr)
		if err == nil {
			debug.Info("Admin %s terminated all %d sessions for user %s", adminID, count, userID)
		}
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"message": "All sessions terminated successfully",
			"count":   count,
		},
	})
}
