package wordlist

import (
	"context"
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

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/fsutil"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/httputil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/mazrean/formstream"
)

// Handler handles wordlist HTTP requests
type Handler struct {
	manager        wordlist.Manager
	potfileService PotfileService
}

// PotfileService interface for potfile operations
type PotfileService interface {
	UpdatePotfileMetadata(ctx context.Context) error
}

// NewHandler creates a new wordlist handler
func NewHandler(manager wordlist.Manager, potfileService PotfileService) *Handler {
	return &Handler{
		manager:        manager,
		potfileService: potfileService,
	}
}

// HandleListWordlists handles requests to list wordlists
func (h *Handler) HandleListWordlists(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters for filtering
	filters := make(map[string]interface{})
	if wordlistType := r.URL.Query().Get("type"); wordlistType != "" {
		filters["wordlist_type"] = wordlistType
	}
	if format := r.URL.Query().Get("format"); format != "" {
		filters["format"] = format
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		filters["tag"] = tag
	}

	// Get wordlists
	wordlists, err := h.manager.ListWordlists(ctx, filters)
	if err != nil {
		debug.Error("Failed to list wordlists: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to list wordlists")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, wordlists)
}

// HandleGetWordlist handles requests to get a wordlist
func (h *Handler) HandleGetWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	// Get wordlist
	wordlist, err := h.manager.GetWordlist(ctx, id)
	if err != nil {
		debug.Error("Failed to get wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get wordlist")
		return
	}

	if wordlist == nil {
		httputil.RespondWithError(w, http.StatusNotFound, "Wordlist not found")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, wordlist)
}

// HandleAddWordlist handles adding a new wordlist (metadata only)
func (h *Handler) HandleAddWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Debug log cookies and headers (sanitized)
	debug.Info("HandleAddWordlist: Processing request with cookies: [REDACTED:cookies:count=%d]", len(r.Cookies()))
	debug.Info("HandleAddWordlist: Request headers: %s", debug.SanitizeHeaders(r.Header))

	// Get user ID from context
	userIDStr, ok := ctx.Value("user_id").(string)
	if !ok {
		debug.Error("HandleAddWordlist: User not authenticated, missing user_id in context")
		httputil.RespondWithError(w, http.StatusUnauthorized, "User not authenticated")
		return
	}

	// Convert string to UUID
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		debug.Error("HandleAddWordlist: Failed to parse user ID as UUID: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Invalid user ID")
		return
	}

	debug.Info("HandleAddWordlist: Authenticated as user: %s", userID.String())

	// Get boundary from Content-Type header
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		debug.Error("HandleAddWordlist: Failed to parse Content-Type: %v", err)
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid Content-Type")
		return
	}
	boundary := params["boundary"]
	if boundary == "" {
		debug.Error("HandleAddWordlist: No boundary in Content-Type")
		httputil.RespondWithError(w, http.StatusBadRequest, "No boundary in Content-Type")
		return
	}

	// Create FormStream parser for efficient streaming
	parser := formstream.NewParser(boundary)

	// Variables to collect form fields
	var (
		name, description, wordlistType, format, tagsStr string
		fileName string
		destFile *os.File
		destPath string
		fileNamePath string
		md5Hash string
		fileSize int64
	)

	// Register handlers for form fields
	parser.Register("name", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		name = string(data)
		debug.Info("HandleAddWordlist: Received name: %s", name)
		return nil
	})

	parser.Register("description", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		description = string(data)
		debug.Info("HandleAddWordlist: Received description: %s", description)
		return nil
	})

	parser.Register("wordlist_type", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		wordlistType = string(data)
		debug.Info("HandleAddWordlist: Received wordlist_type: %s", wordlistType)
		return nil
	})

	parser.Register("format", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		format = string(data)
		debug.Info("HandleAddWordlist: Received format: %s", format)
		return nil
	})

	parser.Register("tags", func(r io.Reader, header formstream.Header) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		tagsStr = string(data)
		debug.Info("HandleAddWordlist: Received tags: %s", tagsStr)
		return nil
	})

	// Register handler for file streaming
	parser.Register("file", func(r io.Reader, header formstream.Header) error {
		fileName = header.FileName()
		debug.Info("HandleAddWordlist: Processing file: %s", fileName)

		// Default wordlist_type to "general" if not provided or empty
		if wordlistType == "" {
			wordlistType = "general"
			debug.Info("HandleAddWordlist: No wordlist_type provided, defaulting to 'general'")
		}

		// Map file extension to database format enum
		dbFormat := "plaintext" // Default to plaintext
		ext := strings.ToLower(filepath.Ext(fileName))
		switch ext {
		case ".gz", ".zip":
			dbFormat = "compressed"
		case ".txt", ".lst", ".dict", "":
			dbFormat = "plaintext"
		}
		format = dbFormat // Set format based on file extension
		debug.Info("HandleAddWordlist: Determined format from extension %s: %s", ext, dbFormat)

		// Use the original filename but sanitize it
		baseFileName := fsutil.SanitizeFilename(fileName)

		// If name is not provided, use the base filename without extension
		if name == "" {
			// Convert to lowercase to match what the monitor does
			name = strings.ToLower(fsutil.ExtractBaseNameWithoutExt(fileName))
		}

		// Create the relative path with subdirectory (matching what the monitor would create)
		fileNamePath = filepath.Join(wordlistType, baseFileName)
		debug.Info("HandleAddWordlist: Using sanitized filename with subdirectory: %s", fileNamePath)

		// Get the destination path for the wordlist file
		destPath = h.manager.GetWordlistPath(fileNamePath, wordlistType)

		// Create the destination directory if it doesn't exist
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			debug.Error("HandleAddWordlist: Failed to create destination directory: %v", err)
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Create destination file
		var err error
		destFile, err = os.Create(destPath)
		if err != nil {
			debug.Error("HandleAddWordlist: Failed to create destination file: %v", err)
			return fmt.Errorf("failed to create file: %w", err)
		}

		// Stream file and calculate MD5 simultaneously
		hasher := md5.New()
		writer := io.MultiWriter(destFile, hasher)
		
		// Use 32KB buffer for streaming to minimize memory usage
		bytesWritten, err := io.CopyBuffer(writer, r, make([]byte, 32*1024))
		if err != nil {
			destFile.Close()
			os.Remove(destPath)
			debug.Error("HandleAddWordlist: Failed to stream file: %v", err)
			return fmt.Errorf("failed to save file: %w", err)
		}

		// Calculate final MD5
		md5Hash = fmt.Sprintf("%x", hasher.Sum(nil))
		fileSize = bytesWritten
		debug.Info("HandleAddWordlist: File streamed successfully: %d bytes, MD5: %s", bytesWritten, md5Hash)

		// Close the file
		if err := destFile.Close(); err != nil {
			debug.Error("HandleAddWordlist: Failed to close destination file: %v", err)
			return fmt.Errorf("failed to close file: %w", err)
		}

		return nil
	})

	// Parse the multipart form
	if err := parser.Parse(r.Body); err != nil {
		debug.Error("HandleAddWordlist: Failed to parse multipart form: %v", err)
		// Clean up partial file if it was created
		if destFile != nil {
			destFile.Close()
		}
		if destPath != "" {
			os.Remove(destPath)
		}
		httputil.RespondWithError(w, http.StatusBadRequest, fmt.Sprintf("Failed to process upload: %v", err))
		return
	}

	// Check if file was actually uploaded
	if fileName == "" {
		debug.Error("HandleAddWordlist: No file provided in multipart form")
		httputil.RespondWithError(w, http.StatusBadRequest, "No file provided")
		return
	}

	debug.Info("HandleAddWordlist: Received form values - name: %s, type: %s, format: %s", name, wordlistType, format)

	// Parse tags
	var tags []string
	if tagsStr != "" {
		tags = strings.Split(tagsStr, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}

	// Check if a wordlist with the same MD5 hash already exists
	existingWordlist, err := h.manager.GetWordlistByMD5Hash(ctx, md5Hash)
	if err != nil {
		debug.Error("Failed to check for duplicate wordlist: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to check for duplicate wordlist")
		return
	}

	if existingWordlist != nil {
		debug.Info("HandleAddWordlist: Duplicate wordlist detected with MD5 hash: %s", md5Hash)
		// Remove the uploaded file since it's a duplicate
		os.Remove(destPath)
		httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
			"id":        existingWordlist.ID,
			"name":      existingWordlist.Name,
			"message":   "Wordlist already exists",
			"duplicate": true,
			"success":   true,
		})
		return
	}

	debug.Info("HandleAddWordlist: No duplicate wordlist found, proceeding with database entry")

	// Check if a file with the same name already exists
	existingWordlistByName, err := h.manager.GetWordlistByFilename(ctx, fileNamePath)
	if err != nil {
		debug.Error("Failed to check for wordlist with same filename: %v", err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to check for existing wordlist")
		return
	}

	var wordlistObj *models.Wordlist

	// If a wordlist with the same name exists
	if existingWordlistByName != nil {
		debug.Info("HandleAddWordlist: Found existing wordlist with same filename: %s", fileNamePath)

		// If the MD5 hash is the same, just return the existing wordlist
		if existingWordlistByName.MD5Hash == md5Hash {
			debug.Info("HandleAddWordlist: Existing wordlist has same MD5 hash, returning existing wordlist")
			// Remove the uploaded file since it's a duplicate
			os.Remove(destPath)
			httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
				"id":        existingWordlistByName.ID,
				"name":      existingWordlistByName.Name,
				"message":   "Wordlist already exists with same content",
				"duplicate": true,
				"success":   true,
			})
				return
		}

		// If the MD5 hash is different, update the existing wordlist
		debug.Info("HandleAddWordlist: Existing wordlist has different MD5 hash, updating")

		// Update the existing wordlist with new file info
		updateReq := &models.WordlistUpdateRequest{
			Name:         name,
			Description:  description,
			WordlistType: wordlistType,
			Format:       format,
			Tags:         append(tags, "updated"),
		}

		if _, err := h.manager.UpdateWordlist(ctx, existingWordlistByName.ID, updateReq, userID); err != nil {
			debug.Error("HandleAddWordlist: Failed to update existing wordlist: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update existing wordlist")
			return
		}

		// We'll continue with the upload process but use the existing wordlist ID
		wordlistObj = existingWordlistByName
	} else {
		// Create wordlist in database
		req := &models.WordlistAddRequest{
			Name:         name,
			Description:  description,
			WordlistType: wordlistType,
			Format:       format,
			FileName:     fileNamePath,
			MD5Hash:      md5Hash,
			FileSize:     fileSize,
			WordCount:    0, // Will be updated during verification
			Tags:         tags,
		}

		// Add wordlist to database
		var err error
		wordlistObj, err = h.manager.AddWordlist(ctx, req, userID)
		if err != nil {
			debug.Error("HandleAddWordlist: Failed to add wordlist: %v", err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to add wordlist")
			return
		}
	}

	// Mark upload as successful to prevent cleanup

	// Automatically trigger verification process
	debug.Info("HandleAddWordlist: Automatically triggering verification for wordlist ID %d", wordlistObj.ID)

	// For large files (over 100MB), perform verification asynchronously
	if fileSize > 100*1024*1024 {
		debug.Info("HandleAddWordlist: Large file detected (%d bytes), starting async verification", fileSize)

		// Start verification in a goroutine
		go func() {
			// Create a new context for the background task
			bgCtx := context.Background()

			debug.Info("HandleAddWordlist: Starting async word count for wordlist ID %d", wordlistObj.ID)

			// Count words in the file
			wordCount, err := h.manager.CountWordsInFile(destPath)
			if err != nil {
				debug.Error("HandleAddWordlist: Async word count failed for wordlist ID %d: %v", wordlistObj.ID, err)

				// Update status to failed if word count fails
				failedStatus := "failed"
				failReq := &models.WordlistVerifyRequest{
					Status: failedStatus,
				}
				if verifyErr := h.manager.VerifyWordlist(bgCtx, wordlistObj.ID, failReq); verifyErr != nil {
					debug.Error("HandleAddWordlist: Failed to update verification status to failed: %v", verifyErr)
				}
				return
			}

			debug.Info("HandleAddWordlist: Async word count completed for wordlist ID %d: %d words", wordlistObj.ID, wordCount)

			// Create verification request
			verifyReq := &models.WordlistVerifyRequest{
				Status:    "verified",
				WordCount: &wordCount,
			}

			// Verify wordlist
			if err := h.manager.VerifyWordlist(bgCtx, wordlistObj.ID, verifyReq); err != nil {
				debug.Error("HandleAddWordlist: Async verification failed for wordlist ID %d: %v", wordlistObj.ID, err)
			} else {
				debug.Info("HandleAddWordlist: Successfully verified wordlist %d with %d words", wordlistObj.ID, wordCount)
			}
		}()

		// Return success response immediately with pending status
		wordlistObj.VerificationStatus = "pending"
		httputil.RespondWithJSON(w, http.StatusCreated, wordlistObj)
		return
	}

	// For smaller files, verify synchronously
	// Count words in the file
	wordCount, err := h.manager.CountWordsInFile(destPath)
	if err != nil {
		debug.Warning("HandleAddWordlist: Failed to count words in file %s: %v", destPath, err)
		// Continue with verification even if word count fails
	}

	// Create verification request
	verifyReq := &models.WordlistVerifyRequest{
		Status:    "verified",
		WordCount: &wordCount,
	}

	// Verify wordlist
	if err := h.manager.VerifyWordlist(ctx, wordlistObj.ID, verifyReq); err != nil {
		debug.Warning("HandleAddWordlist: Failed to automatically verify wordlist %d: %v", wordlistObj.ID, err)
		// Don't fail the upload if verification fails
	} else {
		debug.Info("HandleAddWordlist: Successfully verified wordlist %d with %d words", wordlistObj.ID, wordCount)
		// Update the wordlist object with the verified status and word count for the response
		wordlistObj.VerificationStatus = "verified"
		wordlistObj.WordCount = wordCount
	}

	// Return success response
	httputil.RespondWithJSON(w, http.StatusCreated, wordlistObj)
}

// HandleUpdateWordlist handles requests to update a wordlist
func (h *Handler) HandleUpdateWordlist(w http.ResponseWriter, r *http.Request) {
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

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	// Parse request body
	var req models.WordlistUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update wordlist
	wordlist, err := h.manager.UpdateWordlist(ctx, id, &req, userID)
	if err != nil {
		debug.Error("Failed to update wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update wordlist")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, wordlist)
}

// HandleGetDeletionImpact handles requests to get the impact of deleting a wordlist
func (h *Handler) HandleGetDeletionImpact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	// Get deletion impact
	impact, err := h.manager.GetDeletionImpact(ctx, id)
	if err != nil {
		debug.Error("Failed to get deletion impact for wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get deletion impact")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, impact)
}

// HandleDeleteWordlist handles requests to delete a wordlist
func (h *Handler) HandleDeleteWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
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

	// Delete wordlist
	if err := h.manager.DeleteWordlist(ctx, id, confirmID); err != nil {
		if err == models.ErrResourceInUse {
			// Get the deletion impact to return in the error response
			impact, impactErr := h.manager.GetDeletionImpact(ctx, id)
			if impactErr != nil {
				debug.Error("Failed to get deletion impact: %v", impactErr)
				httputil.RespondWithError(w, http.StatusConflict, "Cannot delete wordlist: it has references that require confirmation")
				return
			}
			// Return 409 with the impact details
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":  "Cannot delete wordlist: it has references that require confirmation",
				"impact": impact,
			})
			return
		}
		debug.Error("Failed to delete wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete wordlist")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Wordlist deleted"})
}

// HandleVerifyWordlist handles requests to verify a wordlist
func (h *Handler) HandleVerifyWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	// Parse request body
	var req models.WordlistVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify wordlist
	if err := h.manager.VerifyWordlist(ctx, id, &req); err != nil {
		debug.Error("Failed to verify wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to verify wordlist")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Wordlist verified"})
}

// HandleAddWordlistTag handles requests to add a tag to a wordlist
func (h *Handler) HandleAddWordlistTag(w http.ResponseWriter, r *http.Request) {
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

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	// Parse request body
	var req models.WordlistTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Add tag
	if err := h.manager.AddWordlistTag(ctx, id, req.Tag, userID); err != nil {
		debug.Error("Failed to add tag to wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to add tag")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Tag added"})
}

// HandleDeleteWordlistTag handles requests to delete a tag from a wordlist
func (h *Handler) HandleDeleteWordlistTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get wordlist ID and tag from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}
	tag := vars["tag"]

	// Delete tag
	if err := h.manager.DeleteWordlistTag(ctx, id, tag); err != nil {
		debug.Error("Failed to delete tag from wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to delete tag")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Tag deleted"})
}

// HandleDownloadWordlist handles requests to download a wordlist
func (h *Handler) HandleDownloadWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	// Get wordlist
	wordlist, err := h.manager.GetWordlist(ctx, id)
	if err != nil {
		debug.Error("Failed to get wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get wordlist")
		return
	}

	if wordlist == nil {
		httputil.RespondWithError(w, http.StatusNotFound, "Wordlist not found")
		return
	}

	// Get file path
	filePath := h.manager.GetWordlistPath(wordlist.FileName, wordlist.WordlistType)
	debug.Info("Downloading wordlist ID %d, filename: %s, path: %s", id, wordlist.FileName, filePath)

	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		debug.Error("Wordlist file not found at path: %s", filePath)
		httputil.RespondWithError(w, http.StatusNotFound, "Wordlist file not found")
		return
	}
	debug.Info("File exists at %s, size: %d bytes", filePath, fileInfo.Size())

	// Set headers
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", wordlist.FileName))
	w.Header().Set("Content-Type", "application/octet-stream")
	// Prevent caching
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	// Don't set Content-Length - let http.ServeFile handle it based on actual file size

	// Log before serving
	debug.Info("About to serve file from path: %s", filePath)
	
	// Serve file
	http.ServeFile(w, r, filePath)

	debug.Info("File served successfully")
}

// HandleRefreshWordlist handles requests to refresh wordlist metadata (MD5, word count, file size)
func (h *Handler) HandleRefreshWordlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get wordlist ID from URL
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		httputil.RespondWithError(w, http.StatusBadRequest, "Invalid wordlist ID")
		return
	}

	debug.Info("Refreshing wordlist ID: %d", id)

	// Get wordlist to check if it's a potfile
	wordlist, err := h.manager.GetWordlist(ctx, id)
	if err != nil {
		debug.Error("Failed to get wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get wordlist")
		return
	}

	if wordlist == nil {
		httputil.RespondWithError(w, http.StatusNotFound, "Wordlist not found")
		return
	}

	// Check if this is the potfile
	if wordlist.IsPotfile {
		debug.Info("Wordlist %d is the potfile, using PotfileService to update metadata", id)

		// Use the potfile service to update metadata
		if h.potfileService != nil {
			if err := h.potfileService.UpdatePotfileMetadata(ctx); err != nil {
				debug.Error("Failed to update potfile metadata: %v", err)
				httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to refresh potfile metadata")
				return
			}
			debug.Info("Successfully refreshed potfile metadata using PotfileService")
		} else {
			debug.Error("PotfileService is not available")
			httputil.RespondWithError(w, http.StatusInternalServerError, "Potfile service not available")
			return
		}
	} else {
		// For regular wordlists, recalculate metadata directly
		debug.Info("Refreshing metadata for regular wordlist %d", id)

		// Get file path
		filePath := h.manager.GetWordlistPath(wordlist.FileName, wordlist.WordlistType)

		// Check if file exists
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			debug.Error("Failed to stat wordlist file at %s: %v", filePath, err)
			httputil.RespondWithError(w, http.StatusNotFound, "Wordlist file not found")
			return
		}

		// Calculate MD5 hash
		md5Hash, err := h.manager.CalculateFileMD5(filePath)
		if err != nil {
			debug.Error("Failed to calculate MD5 for wordlist %d: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to calculate MD5 hash")
			return
		}

		// Count words in file
		wordCount, err := h.manager.CountWordsInFile(filePath)
		if err != nil {
			debug.Error("Failed to count words for wordlist %d: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to count words")
			return
		}

		// Update wordlist metadata in database
		if err := h.manager.UpdateWordlistComplete(ctx, id, md5Hash, fileInfo.Size(), wordCount); err != nil {
			debug.Error("Failed to update wordlist metadata for ID %d: %v", id, err)
			httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to update wordlist metadata")
			return
		}

		debug.Info("Successfully refreshed metadata for wordlist %d - MD5: %s, Size: %d, Words: %d",
			id, md5Hash, fileInfo.Size(), wordCount)
	}

	// Get updated wordlist
	updatedWordlist, err := h.manager.GetWordlist(ctx, id)
	if err != nil {
		debug.Error("Failed to get updated wordlist %d: %v", id, err)
		httputil.RespondWithError(w, http.StatusInternalServerError, "Failed to get updated wordlist")
		return
	}

	httputil.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "Wordlist metadata refreshed successfully",
		"wordlist": updatedWordlist,
	})
}
