package rule

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/fsutil"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/mazrean/formstream"
)

// Handler handles rule management HTTP requests
type Handler struct {
	manager rule.Manager
	config  *config.Config
}

// NewHandler creates a new rule management handler
func NewHandler(manager rule.Manager, cfg *config.Config) *Handler {
	return &Handler{
		manager: manager,
		config:  cfg,
	}
}

// Request/Response types
type AddRuleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RuleType    string   `json:"rule_type"`
	Tags        []string `json:"tags,omitempty"`
}

type RuleResponse struct {
	ID                 int      `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	RuleType           string   `json:"rule_type"`
	FileName           string   `json:"file_name"`
	MD5Hash            string   `json:"md5_hash"`
	FileSize           int64    `json:"file_size"`
	RuleCount          int64    `json:"rule_count"`
	CreatedAt          string   `json:"created_at"`
	CreatedBy          string   `json:"created_by"`
	UpdatedAt          string   `json:"updated_at"`
	UpdatedBy          string   `json:"updated_by,omitempty"`
	LastVerifiedAt     string   `json:"last_verified_at,omitempty"`
	VerificationStatus string   `json:"verification_status"`
	Tags               []string `json:"tags,omitempty"`
}

type AddTagRequest struct {
	Tag string `json:"tag"`
}

// HandleListRules handles listing all rules
func (h *Handler) HandleListRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters for filtering
	filters := make(map[string]interface{})
	if ruleType := r.URL.Query().Get("type"); ruleType != "" {
		filters["rule_type"] = ruleType
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filters["tag"] = tag
	}

	// Get rules from database
	rules, err := h.manager.ListRules(ctx, filters)
	if err != nil {
		debug.Error("Failed to list rules: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list rules")
		return
	}

	// Convert rules to response format
	response := make([]RuleResponse, len(rules))
	for i, rule := range rules {
		response[i] = convertRuleToResponse(rule)
	}

	httputil.RespondWithJSON(w, http.StatusOK, response)
}

// HandleListRulesForAgent handles listing rules for agents
func (h *Handler) HandleListRulesForAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters for filtering
	filters := make(map[string]interface{})
	if ruleType := r.URL.Query().Get("type"); ruleType != "" {
		filters["rule_type"] = ruleType
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filters["tag"] = tag
	}

	// Only return verified rules for agents
	filters["verification_status"] = "verified"

	// Get rules from database
	rules, err := h.manager.ListRules(ctx, filters)
	if err != nil {
		debug.Error("Failed to list rules: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list rules")
		return
	}

	// Convert rules to response format
	response := make([]RuleResponse, len(rules))
	for i, rule := range rules {
		response[i] = convertRuleToResponse(rule)
	}

	httputil.RespondWithJSON(w, http.StatusOK, response)
}

// HandleGetRule handles getting a rule by ID
func (h *Handler) HandleGetRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get rule ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Rule ID is required")
		return
	}

	// Convert ID to int
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	rule, err := h.manager.GetRule(ctx, id)
	if err != nil {
		debug.Error("Failed to get rule %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get rule")
		return
	}

	if rule == nil {
		httputil.RespondWithError(w, http.StatusNotFound, "Rule not found")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, convertRuleToResponse(rule))
}

// HandleAddRule handles the request to add a new rule
// ruleCounter counts non-empty, non-comment lines
type ruleCounter struct {
	count int64
	buf   []byte
}

func (rc *ruleCounter) Write(p []byte) (n int, err error) {
	n = len(p)
	rc.buf = append(rc.buf, p...)

	// Process complete lines
	for {
		idx := bytes.IndexByte(rc.buf, '\n')
		if idx == -1 {
			break
		}

		line := bytes.TrimSpace(rc.buf[:idx])
		if len(line) > 0 && !bytes.HasPrefix(line, []byte("#")) {
			rc.count++
		}

		rc.buf = rc.buf[idx+1:]
	}

	return n, nil
}

func (h *Handler) HandleAddRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userIDStr, ok := ctx.Value("user_id").(string)
	if !ok {
		debug.Error("Failed to get user ID from context")
		httputil.RespondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Convert string to UUID
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Failed to parse user ID as UUID: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Invalid user ID")
		return
	}

	// Get boundary from Content-Type header
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		debug.Error("Failed to parse Content-Type: %v", err)
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid Content-Type")
		return
	}
	boundary := params["boundary"]
	if boundary == "" {
		debug.Error("No boundary in Content-Type")
		httputil.RespondWithError(w, http.StatusBadRequest, "No boundary in Content-Type")
		return
	}

	// Create FormStream parser for efficient streaming
	parser := formstream.NewParser(boundary)

	// Variables to collect form fields
	var (
		ruleName, description, ruleType, tagsStr string
		fileName                                 string
		md5Hash                                  string
		fileSize                                 int64
		ruleCount                                int64
		destPath                                 string
		fileNamePath                             string
	)

	// Register handlers for form fields
	parser.Register("name", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		ruleName = string(data)
		debug.Info("HandleAddRule: Received name: %s", ruleName)
		return nil
	})

	parser.Register("description", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		description = string(data)
		debug.Info("HandleAddRule: Received description: %s", description)
		return nil
	})

	parser.Register("rule_type", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		ruleType = string(data)
		debug.Info("HandleAddRule: Received rule_type: %s", ruleType)
		return nil
	})

	parser.Register("tags", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		tagsStr = string(data)
		debug.Info("HandleAddRule: Received tags: %s", tagsStr)
		return nil
	})

	// Register handler for file streaming
	parser.Register("file", func(r io.Reader, header formstream.Header) error {
		fileName = header.FileName()
		debug.Info("HandleAddRule: Processing file: %s", fileName)

		// Create a temporary file to store the content
		// We'll move it to the correct location after we have the rule_type
		tempFile, err := os.CreateTemp("", "rule_upload_*.tmp")
		if err != nil {
			debug.Error("HandleAddRule: Failed to create temp file: %v", err)
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tempPath := tempFile.Name()
		defer tempFile.Close()

		// Create a tee reader to count rules while streaming
		hasher := md5.New()
		lineCounter := &ruleCounter{}
		writer := io.MultiWriter(tempFile, hasher, lineCounter)

		// Stream file to temp location and calculate MD5 simultaneously
		bytesWritten, err := io.CopyBuffer(writer, r, make([]byte, 32*1024))
		if err != nil {
			os.Remove(tempPath)
			debug.Error("HandleAddRule: Failed to stream file: %v", err)
			return fmt.Errorf("failed to save file: %w", err)
		}

		// Store results for later use
		md5Hash = fmt.Sprintf("%x", hasher.Sum(nil))
		fileSize = bytesWritten
		ruleCount = lineCounter.count
		destPath = tempPath // Store temp path for now
		debug.Info("HandleAddRule: File streamed to temp location: %d bytes, MD5: %s, Rules: %d", bytesWritten, md5Hash, ruleCount)

		return nil
	})

	// Parse the multipart form
	if err := parser.Parse(r.Body); err != nil {
		debug.Error("HandleAddRule: Failed to parse multipart form: %v", err)
		// Clean up partial file if it was created
		if destPath != "" {
			os.Remove(destPath)
		}
		httputil.RespondWithError(w, http.StatusBadRequest, fmt.Sprintf("Failed to process upload: %v", err))
		return
	}

	// Check if file was actually uploaded
	if fileName == "" {
		debug.Error("HandleAddRule: No file provided in multipart form")
		httputil.RespondWithError(w, http.StatusBadRequest, "No file provided")
		return
	}

	// Check if rule_type was provided
	if ruleType == "" {
		// Clean up temp file
		if destPath != "" {
			os.Remove(destPath)
		}
		debug.Error("HandleAddRule: Rule type is required")
		httputil.RespondWithError(w, http.StatusBadRequest, "Rule type is required")
		return
	}

	// Now that we have the rule_type, determine the final path
	// If name is not provided, use the base filename without extension
	if ruleName == "" {
		// Convert to lowercase to match what the monitor does
		ruleName = strings.ToLower(fsutil.ExtractBaseNameWithoutExt(fileName))
	}

	// Use the original filename but sanitize it
	baseFileName := fsutil.SanitizeFilename(fileName)

	// Create the relative path with subdirectory (matching what the monitor would create)
	fileNamePath = filepath.Join(ruleType, baseFileName)
	debug.Info("HandleAddRule: Using sanitized filename with subdirectory: %s", fileNamePath)

	// Get the final destination path for the rule file
	finalPath := h.manager.GetRulePath(fileNamePath, ruleType)

	// Create the destination directory if it doesn't exist
	destDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		// Clean up temp file
		os.Remove(destPath)
		debug.Error("HandleAddRule: Failed to create destination directory: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to create directory")
		return
	}

	// Move temp file to final destination
	if err := os.Rename(destPath, finalPath); err != nil {
		// If rename fails (e.g., across filesystems), copy and delete
		srcFile, err := os.Open(destPath)
		if err != nil {
			os.Remove(destPath)
			debug.Error("HandleAddRule: Failed to open temp file: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to move file")
			return
		}
		defer srcFile.Close()

		dstFile, err := os.Create(finalPath)
		if err != nil {
			os.Remove(destPath)
			debug.Error("HandleAddRule: Failed to create destination file: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to create file")
			return
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			os.Remove(destPath)
			os.Remove(finalPath)
			debug.Error("HandleAddRule: Failed to copy file: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to save file")
			return
		}

		// Remove temp file after successful copy
		os.Remove(destPath)
	}

	// Update destPath to the final location
	destPath = finalPath
	debug.Info("HandleAddRule: File moved to final location: %s", destPath)

	// Parse tags
	var tags []string
	if tagsStr != "" {
		tags = strings.Split(tagsStr, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}

	debug.Info("HandleAddRule: Checking for duplicate rule with MD5 hash: %s", md5Hash)

	// Check if a rule with the same MD5 hash already exists
	existingRule, err := h.manager.GetRuleByMD5Hash(ctx, md5Hash)
	if err != nil {
		debug.Error("Failed to check for duplicate rule: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to check for duplicate rule")
		return
	}

	if existingRule != nil {
		debug.Info("HandleAddRule: Duplicate rule detected with MD5 hash: %s", md5Hash)
		// Remove the uploaded file since it's a duplicate
		os.Remove(destPath)
		httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"id":        existingRule.ID,
			"name":      existingRule.Name,
			"message":   "Rule already exists",
			"duplicate": true,
			"success":   true,
		})
		return
	}

	debug.Info("HandleAddRule: No duplicate rule found, proceeding with database entry")

	// Check if a file with the same name already exists
	existingRuleByName, err := h.manager.GetRuleByFilename(ctx, fileNamePath)
	if err != nil {
		debug.Error("Failed to check for rule with same filename: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to check for existing rule")
		return
	}

	var ruleObj *models.Rule

	// If a rule with the same name exists
	if existingRuleByName != nil {
		debug.Info("HandleAddRule: Found existing rule with same filename: %s", fileNamePath)

		// If the MD5 hash is the same, just return the existing rule
		if existingRuleByName.MD5Hash == md5Hash {
			debug.Info("HandleAddRule: Existing rule has same MD5 hash, returning existing rule")
			httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
				"id":        existingRuleByName.ID,
				"name":      existingRuleByName.Name,
				"message":   "Rule already exists with same content",
				"duplicate": true,
				"success":   true,
			})
			return
		}

		// If the MD5 hash is different, update the existing rule
		debug.Info("HandleAddRule: Existing rule has different MD5 hash, updating")

		// Update the existing rule with new file info
		updateReq := &models.RuleUpdateRequest{
			Name:        ruleName,
			Description: description,
			RuleType:    ruleType,
			Tags:        append(tags, "updated"),
		}

		if _, err := h.manager.UpdateRule(ctx, existingRuleByName.ID, updateReq, userID); err != nil {
			debug.Error("HandleAddRule: Failed to update existing rule: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update existing rule")
			return
		}

		// We'll continue with the upload process but use the existing rule ID
		ruleObj = existingRuleByName
	} else {
		// Create rule in database
		req := &models.RuleAddRequest{
			Name:        ruleName,
			Description: description,
			RuleType:    ruleType,
			FileName:    fileNamePath,
			MD5Hash:     md5Hash,
			FileSize:    fileSize,
			RuleCount:   ruleCount,
			Tags:        tags,
		}

		// Add rule to database
		var err error
		ruleObj, err = h.manager.AddRule(ctx, req, userID)
		if err != nil {
			debug.Error("Failed to add rule to database: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to add rule")
			return
		}
	}

	// Perform verification (if needed)
	verifyReq := &models.RuleVerifyRequest{
		Status:    "verified",
		RuleCount: &ruleCount,
	}

	if err := h.manager.VerifyRule(ctx, ruleObj.ID, verifyReq); err != nil {
		debug.Warning("Failed to verify rule: %v", err)
		// Don't fail the upload if verification fails
	} else {
		ruleObj.VerificationStatus = "verified"
	}

	httputil.RespondWithJSON(w, http.StatusCreated, ruleObj)
}

// HandleUpdateRule handles updating a rule
func (h *Handler) HandleUpdateRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userIDStr, ok := ctx.Value("user_id").(string)
	if !ok {
		debug.Error("Failed to get user ID from context")
		httputil.RespondWithError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	// Convert string to UUID
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Failed to parse user ID as UUID: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Invalid user ID")
		return
	}

	// Get rule ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Rule ID is required")
		return
	}

	// Convert ID to int
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	// Parse request body
	var req models.RuleUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update rule
	rule, err := h.manager.UpdateRule(ctx, id, &req, userID)
	if err != nil {
		debug.Error("Failed to update rule %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update rule")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, convertRuleToResponse(rule))
}

// HandleGetDeletionImpact handles requests to get the impact of deleting a rule
func (h *Handler) HandleGetDeletionImpact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get rule ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	// Get deletion impact
	impact, err := h.manager.GetDeletionImpact(ctx, id)
	if err != nil {
		debug.Error("Failed to get deletion impact for rule %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get deletion impact")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, impact)
}

// HandleDeleteRule handles deleting a rule
func (h *Handler) HandleDeleteRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get rule ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Rule ID is required")
		return
	}

	// Convert ID to int
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	// Parse request body for confirm_id (optional)
	var confirmID *int
	if r.Body != nil && r.ContentLength > 0 {
		var req models.DeleteResourceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.ConfirmID != nil {
			confirmID = req.ConfirmID
		}
	}

	// Delete rule
	if err := h.manager.DeleteRule(ctx, id, confirmID); err != nil {
		if err == models.ErrResourceInUse {
			// Get the deletion impact to return in the error response
			impact, impactErr := h.manager.GetDeletionImpact(ctx, id)
			if impactErr != nil {
				debug.Error("Failed to get deletion impact: %v", impactErr)
				httputil.RespondWithError(w, http.StatusConflict, "Cannot delete rule: it has references that require confirmation")
				return
			}
			// Return 409 with the impact details
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":  "Cannot delete rule: it has references that require confirmation",
				"impact": impact,
			})
			return
		}
		debug.Error("Failed to delete rule %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete rule")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Rule deleted successfully"})
}

// HandleVerifyRule handles verifying a rule
func (h *Handler) HandleVerifyRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get rule ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Rule ID is required")
		return
	}

	// Convert ID to int
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	// Parse request body
	var req models.RuleVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify rule
	if err := h.manager.VerifyRule(ctx, id, &req); err != nil {
		debug.Error("Failed to verify rule %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to verify rule")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Rule verified successfully"})
}

// HandleUploadRule handles uploading a rule file

// HandleGetRuleTags handles getting tags for a rule
func (h *Handler) HandleGetRuleTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	// Get rule to access its tags
	rule, err := h.manager.GetRule(ctx, id)
	if err != nil {
		debug.Error("Failed to get rule %d: %v", id, err)
		http.Error(w, "Failed to get rule", http.StatusInternalServerError)
		return
	}

	if rule == nil {
		http.Error(w, "Rule not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule.Tags)
}

// HandleAddRuleTag handles adding a tag to a rule
func (h *Handler) HandleAddRuleTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context
	userIDStr, ok := ctx.Value("user_id").(string)
	if !ok {
		debug.Error("Failed to get user ID from context")
		httputil.RespondWithError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	// Convert string to UUID
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("Failed to parse user ID as UUID: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Invalid user ID")
		return
	}

	// Get rule ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Rule ID is required")
		return
	}

	// Convert ID to int
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	// Parse request body
	var req struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Add tag
	if err := h.manager.AddRuleTag(ctx, id, req.Tag, userID); err != nil {
		debug.Error("Failed to add tag to rule %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to add tag")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Tag added successfully"})
}

// HandleDeleteRuleTag handles deleting a tag from a rule
func (h *Handler) HandleDeleteRuleTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get rule ID and tag from URL
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Rule ID is required")
		return
	}
	tag := vars["tag"]
	if tag == "" {
		httputil.RespondWithError(w, http.StatusBadRequest, "Tag is required")
		return
	}

	// Convert ID to int
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid rule ID")
		return
	}

	// Delete tag
	if err := h.manager.DeleteRuleTag(ctx, id, tag); err != nil {
		debug.Error("Failed to delete tag from rule %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete tag")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Tag deleted successfully"})
}

// HandleDownloadRule handles downloading a rule file
func (h *Handler) HandleDownloadRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get filename from URL
	vars := mux.Vars(r)
	filename := vars["filename"]
	if filename == "" {
		// Try to get rule ID from URL
		idStr := vars["id"]
		if idStr == "" {
			http.Error(w, "Missing rule ID or filename", http.StatusBadRequest)
			return
		}

		// Convert ID to int
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid rule ID", http.StatusBadRequest)
			return
		}

		// Get rule by ID
		rule, err := h.manager.GetRule(ctx, id)
		if err != nil {
			debug.Error("Failed to get rule %d: %v", id, err)
			http.Error(w, "Rule not found", http.StatusNotFound)
			return
		}

		filename = rule.FileName
	}

	// Get rule by filename
	rule, err := h.manager.GetRuleByFilename(ctx, filename)
	if err != nil {
		debug.Error("Failed to get rule by filename %s: %v", filename, err)
		http.Error(w, "Rule not found", http.StatusNotFound)
		return
	}

	// Get file path
	filePath := h.manager.GetRulePath(rule.FileName, rule.RuleType)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		debug.Error("Rule file not found: %s", filePath)
		http.Error(w, "Rule file not found", http.StatusNotFound)
		return
	}

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		debug.Error("Failed to open rule file %s: %v", filePath, err)
		http.Error(w, "Failed to open rule file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set headers
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", rule.FileSize))

	// Stream file to response
	if _, err := io.Copy(w, file); err != nil {
		debug.Error("Failed to stream rule file %s: %v", filePath, err)
		// Can't send error response here as headers are already sent
	}
}

// Helper function to convert a rule to response format
func convertRuleToResponse(rule *models.Rule) RuleResponse {
	response := RuleResponse{
		ID:                 rule.ID,
		Name:               rule.Name,
		Description:        rule.Description,
		RuleType:           rule.RuleType,
		FileName:           rule.FileName,
		MD5Hash:            rule.MD5Hash,
		FileSize:           rule.FileSize,
		RuleCount:          rule.RuleCount,
		CreatedAt:          rule.CreatedAt.Format(time.RFC3339),
		CreatedBy:          rule.CreatedBy.String(),
		UpdatedAt:          rule.UpdatedAt.Format(time.RFC3339),
		VerificationStatus: rule.VerificationStatus,
		Tags:               rule.Tags,
	}

	// Add optional fields if present
	if rule.UpdatedBy != uuid.Nil {
		response.UpdatedBy = rule.UpdatedBy.String()
	}

	if !rule.LastVerifiedAt.IsZero() {
		response.LastVerifiedAt = rule.LastVerifiedAt.Format(time.RFC3339)
	}

	return response
}
