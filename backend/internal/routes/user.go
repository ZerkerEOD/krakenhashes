package routes

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/agent"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/jobs"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/user"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// UserJobsHandlerInstance is a global reference to the user jobs handler
// This is a temporary solution to allow setting the WebSocket handler after initialization
var UserJobsHandlerInstance *jobs.UserJobsHandler

// CreateJobsHandler creates and returns the jobs handler
func CreateJobsHandler(database *db.DB, dataDir string, binaryManager binary.Manager) *jobs.UserJobsHandler {
	// Create repositories
	dbWrapper := &db.DB{DB: database.DB}
	jobExecRepo := repository.NewJobExecutionRepository(dbWrapper)
	jobTaskRepo := repository.NewJobTaskRepository(dbWrapper)
	hashlistRepo := repository.NewHashListRepository(dbWrapper)
	presetJobRepo := repository.NewPresetJobRepository(database.DB)
	benchmarkRepo := repository.NewBenchmarkRepository(dbWrapper)
	agentHashlistRepo := repository.NewAgentHashlistRepository(dbWrapper)
	agentRepo := repository.NewAgentRepository(dbWrapper)
	systemSettingsRepo := repository.NewSystemSettingsRepository(dbWrapper)
	fileRepo := repository.NewFileRepository(dbWrapper, dataDir)
	hashTypeRepo := repository.NewHashTypeRepository(dbWrapper)

	// Create client repository
	clientRepo := repository.NewClientRepository(dbWrapper)

	// Create device repository
	deviceRepo := repository.NewAgentDeviceRepository(dbWrapper)

	// Create schedule repository
	scheduleRepo := repository.NewAgentScheduleRepository(dbWrapper)

	// Create additional repositories for job creation
	workflowRepo := repository.NewJobWorkflowRepository(database.DB)
	wordlistStore := wordlist.NewStore(database.DB)
	ruleStore := rule.NewStore(database.DB)
	binaryStore := binary.NewStore(database.DB)
	jobIncrementLayerRepo := repository.NewJobIncrementLayerRepository(dbWrapper)
	presetIncrementLayerRepo := repository.NewPresetIncrementLayerRepository(dbWrapper)

	// Create job execution service
	jobExecutionService := services.NewJobExecutionService(
		database,
		jobExecRepo,
		jobTaskRepo,
		jobIncrementLayerRepo,
		presetIncrementLayerRepo,
		benchmarkRepo,
		agentHashlistRepo,
		agentRepo,
		deviceRepo,
		presetJobRepo,
		hashlistRepo,
		systemSettingsRepo,
		fileRepo,
		scheduleRepo,
		binaryManager,
		"", // hashcatBinaryPath - not needed for keyspace calculation
		dataDir,
	)

	// Create jobs handler
	return jobs.NewUserJobsHandler(
		jobExecRepo,
		jobTaskRepo,
		jobIncrementLayerRepo,
		presetJobRepo,
		hashlistRepo,
		clientRepo,
		workflowRepo,
		hashTypeRepo,
		wordlistStore,
		ruleStore,
		binaryStore,
		jobExecutionService,
		systemSettingsRepo,
	)
}

// SetupUserRoutes configures all user-related routes
func SetupUserRoutes(router *mux.Router, database *db.DB, dataDir string, binaryManager binary.Manager, agentService *services.AgentService) {
	debug.Info("Setting up user routes")

	// Create handlers
	jobsHandler := CreateJobsHandler(database, dataDir, binaryManager)

	// Store the handler globally so we can set the WebSocket handler later
	UserJobsHandlerInstance = jobsHandler
	dbWrapper := &db.DB{DB: database.DB}
	userHandler := user.NewHandler(dbWrapper)
	agentHandler := agent.NewAgentHandler(agentService)

	// SSE removed - using polling instead
	// The frontend now polls /jobs endpoint every 5 seconds for updates

	// User listing route (for agent owner selection)
	router.HandleFunc("/users", userHandler.ListUsers).Methods("GET", "OPTIONS")

	// User-specific jobs route
	router.HandleFunc("/user/jobs", jobsHandler.ListUserJobs).Methods("GET", "OPTIONS")
	
	// User-specific agents route with current task info
	router.HandleFunc("/user/agents", agentHandler.GetUserAgents).Methods("GET", "OPTIONS")

	// IMPORTANT: Register specific routes before generic patterns to avoid conflicts

	// Other specific job routes (before generic {id} pattern)
	router.HandleFunc("/jobs/finished", jobsHandler.DeleteFinishedJobs).Methods("DELETE", "OPTIONS")

	// Generic job routes (MUST come after specific routes)
	router.HandleFunc("/jobs", jobsHandler.ListJobs).Methods("GET", "OPTIONS")
	router.HandleFunc("/jobs/{id}", jobsHandler.GetJobDetail).Methods("GET", "OPTIONS")
	router.HandleFunc("/jobs/{id}", jobsHandler.UpdateJob).Methods("PATCH", "OPTIONS")
	router.HandleFunc("/jobs/{id}/retry", jobsHandler.RetryJob).Methods("POST", "OPTIONS")
	router.HandleFunc("/jobs/{id}/force-complete", jobsHandler.ForceCompleteJob).Methods("POST", "OPTIONS")
	router.HandleFunc("/jobs/{id}/tasks/{taskId}/retry", jobsHandler.RetryTask).Methods("POST", "OPTIONS")
	router.HandleFunc("/jobs/{id}", jobsHandler.DeleteJob).Methods("DELETE", "OPTIONS")

	// Increment layer routes
	router.HandleFunc("/jobs/{id}/layers", jobsHandler.GetJobLayers).Methods("GET", "OPTIONS")
	router.HandleFunc("/jobs/{id}/layers/{layer_id}/tasks", jobsHandler.GetJobLayerTasks).Methods("GET", "OPTIONS")

	// Get user profile
	router.HandleFunc("/user/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get user ID from context (set by RequireAuth middleware)
		userID := r.Context().Value("user_id").(string)
		if userID == "" {
			debug.Warning("No user ID found in context")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Query user profile
		var profile struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Email    string `json:"email"`
			Role     string `json:"role"`
		}

		err := database.QueryRow(`
			SELECT id, username, email, role
			FROM users
			WHERE id = $1
		`, userID).Scan(&profile.ID, &profile.Username, &profile.Email, &profile.Role)

		if err != nil {
			debug.Error("Failed to get user profile: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profile)
	}).Methods("GET")

	// Update user profile
	router.HandleFunc("/user/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get user ID from context (set by RequireAuth middleware)
		userID := r.Context().Value("user_id").(string)
		if userID == "" {
			debug.Warning("No user ID found in context")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse request
		var req struct {
			Email           string `json:"email,omitempty"`
			CurrentPassword string `json:"currentPassword,omitempty"`
			NewPassword     string `json:"newPassword,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			debug.Error("Failed to parse profile update request: %v", err)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// If any changes are being made, require current password
		if req.Email != "" || req.NewPassword != "" {
			if req.CurrentPassword == "" {
				http.Error(w, "Current password is required", http.StatusBadRequest)
				return
			}

			// Get current password hash
			var currentHash string
			err := database.QueryRow("SELECT password_hash FROM users WHERE id = $1", userID).Scan(&currentHash)
			if err != nil {
				debug.Error("Failed to get user password: %v", err)
				http.Error(w, "User not found", http.StatusNotFound)
				return
			}

			// Verify current password
			if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
				http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
				return
			}
		}

		// Update email if provided
		if req.Email != "" {
			// Basic email validation
			if !strings.Contains(req.Email, "@") {
				http.Error(w, "Invalid email address", http.StatusBadRequest)
				return
			}

			_, err := database.Exec("UPDATE users SET email = $1, updated_at = NOW() WHERE id = $2", req.Email, userID)
			if err != nil {
				debug.Error("Failed to update email: %v", err)
				http.Error(w, "Failed to update email", http.StatusInternalServerError)
				return
			}
		}

		// Update password if provided
		if req.NewPassword != "" {
			// Validate new password (basic validation)
			if len(req.NewPassword) < 8 {
				http.Error(w, "Password must be at least 8 characters long", http.StatusBadRequest)
				return
			}

			// Hash new password
			newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
			if err != nil {
				debug.Error("Failed to hash password: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Update password
			_, err = database.Exec("UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2", newHash, userID)
			if err != nil {
				debug.Error("Failed to update password: %v", err)
				http.Error(w, "Failed to update password", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Profile updated successfully"})
	}).Methods("PUT", "OPTIONS")

	// Notification preferences
	notificationHandler := user.NewNotificationPreferencesHandler(database.DB)
	router.HandleFunc("/user/notification-preferences", notificationHandler.GetNotificationPreferences).Methods("GET")
	router.HandleFunc("/user/notification-preferences", notificationHandler.UpdateNotificationPreferences).Methods("PUT")

	// Update password
	router.HandleFunc("/user/password", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get user ID from context
		userID := r.Context().Value("user_id").(string)
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse request
		var req struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Validate new password (basic validation)
		if len(req.NewPassword) < 8 {
			http.Error(w, "Password must be at least 8 characters long", http.StatusBadRequest)
			return
		}

		// Get current password hash
		var currentHash string
		err := database.QueryRow("SELECT password_hash FROM users WHERE id = $1", userID).Scan(&currentHash)
		if err != nil {
			debug.Error("Failed to get user password: %v", err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		// Verify current password
		if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
			http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
			return
		}

		// Hash new password
		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			debug.Error("Failed to hash password: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Update password
		_, err = database.Exec("UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2", newHash, userID)
		if err != nil {
			debug.Error("Failed to update password: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Password updated successfully"})
	}).Methods("POST")

	// Generate new refresh token
	router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Validate refresh token
		var userID string
		var expiresAt time.Time
		err := database.QueryRow(`
			SELECT user_id, expires_at 
			FROM auth_tokens 
			WHERE token = $1 AND token_type = 'refresh' AND revoked = false
		`, req.RefreshToken).Scan(&userID, &expiresAt)

		if err == sql.ErrNoRows {
			http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
			return
		}
		if err != nil {
			debug.Error("Failed to validate refresh token: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Check if token is expired
		if time.Now().After(expiresAt) {
			http.Error(w, "Refresh token expired", http.StatusUnauthorized)
			return
		}

		// Get user role
		var role string
		err = database.QueryRow("SELECT role FROM users WHERE id = $1", userID).Scan(&role)
		if err != nil {
			debug.Error("Failed to get user role: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Get JWT expiry from auth settings
		authSettings, err := database.GetAuthSettings()
		if err != nil {
			debug.Error("Failed to get auth settings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Generate new access token
		accessToken, err := jwt.GenerateToken(userID, role, authSettings.JWTExpiryMinutes)
		if err != nil {
			debug.Error("Failed to generate access token: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"accessToken": accessToken,
		})
	}).Methods("POST")

	// User API Key management routes
	userRepo := repository.NewUserRepository(database)
	userAPIService := services.NewUserAPIService(userRepo)
	apiKeyHandler := user.NewAPIKeyHandler(userAPIService)

	router.HandleFunc("/user/api-key", apiKeyHandler.GenerateAPIKey).Methods("POST", "OPTIONS")
	router.HandleFunc("/user/api-key", apiKeyHandler.RevokeAPIKey).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/user/api-key/info", apiKeyHandler.GetAPIKeyInfo).Methods("GET", "OPTIONS")
	debug.Info("Configured user API key management routes")
}
