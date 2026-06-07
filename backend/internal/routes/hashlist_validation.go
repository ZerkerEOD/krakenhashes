package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// handleConfirmValidation finalizes a hashlist that's currently paused in
// the 'awaiting_validation_decision' state (GitHub issue #38). The user
// chooses `proceed` to keep the valid lines and start processing, or
// `cancel` to delete the upload entirely.
func (h *hashlistHandler) handleConfirmValidation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, "Invalid hashlist id", http.StatusBadRequest)
		return
	}

	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	hashlist, err := h.hashlistRepo.GetByID(ctx, id)
	if err != nil {
		jsonError(w, "Hashlist not found", http.StatusNotFound)
		return
	}
	if hashlist.Status != models.HashListStatusAwaitingValidationDecision {
		jsonError(w, "Hashlist is not awaiting validation decision (current status: "+hashlist.Status+")", http.StatusConflict)
		return
	}

	switch body.Action {
	case "proceed":
		hashlistPath := findUploadFile(h.dataDir, hashlist.ID)
		if hashlistPath == "" {
			debug.Error("Could not locate uploaded file for hashlist %d under %s", hashlist.ID, h.dataDir)
			jsonError(w, "Uploaded file not found on disk; cannot resume processing", http.StatusInternalServerError)
			return
		}
		if err := h.hashlistRepo.UpdateStatus(ctx, hashlist.ID, models.HashListStatusProcessing, ""); err != nil {
			debug.Error("Failed to transition hashlist %d to processing: %v", hashlist.ID, err)
			jsonError(w, "Failed to update hashlist status", http.StatusInternalServerError)
			return
		}
		go h.processor.SubmitHashlistForProcessing(hashlist.ID, hashlistPath)
		jsonResponse(w, http.StatusAccepted, map[string]interface{}{
			"hashlist_id": hashlist.ID,
			"status":      models.HashListStatusProcessing,
		})
		return

	case "cancel":
		if hashlistPath := findUploadFile(h.dataDir, hashlist.ID); hashlistPath != "" {
			if err := os.Remove(hashlistPath); err != nil && !os.IsNotExist(err) {
				debug.Warning("Failed to remove cancelled hashlist file %s: %v", hashlistPath, err)
			}
		}
		// Mark as cancelled before deletion so audit trail is preserved if
		// the delete fails. Delete cascades to invalid_hashes via FK.
		if err := h.hashlistRepo.UpdateStatus(ctx, hashlist.ID, models.HashListStatusCancelled, ""); err != nil {
			debug.Error("Failed to mark hashlist %d as cancelled: %v", hashlist.ID, err)
			jsonError(w, "Failed to update hashlist status", http.StatusInternalServerError)
			return
		}
		if err := h.hashlistRepo.Delete(ctx, hashlist.ID); err != nil {
			debug.Error("Failed to delete cancelled hashlist %d: %v", hashlist.ID, err)
			// Don't fail the request — the row is marked cancelled and won't
			// be acted on. Operators can clean up on a slow path.
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"hashlist_id": hashlist.ID,
			"status":      models.HashListStatusCancelled,
		})
		return

	default:
		jsonError(w, "action must be \"proceed\" or \"cancel\"", http.StatusBadRequest)
		return
	}
}

// flattenHashlistResponse merges a HashList's JSON-encoded fields with the
// caller's extras at the top level, so existing API consumers that read
// `response.id` / `response.status` continue to work when we add new sibling
// fields (validation summary) onto the upload response (GitHub issue #38).
func flattenHashlistResponse(hashlist *models.HashList, extras map[string]interface{}) ([]byte, error) {
	raw, err := json.Marshal(hashlist)
	if err != nil {
		return nil, err
	}
	var merged map[string]interface{}
	if err := json.Unmarshal(raw, &merged); err != nil {
		return nil, err
	}
	for k, v := range extras {
		merged[k] = v
	}
	return json.Marshal(merged)
}

// pointerString returns the dereferenced string or nil so callers building
// a JSON map can produce a clean string-or-null without exposing a
// sql.NullString {"String":…,"Valid":…} shape.
func pointerString(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// findUploadFile globs the data directory for the temp upload file named
// "{id}_*" — the prefix the upload handler writes. Returns the absolute path
// or "" when no match is found.
func findUploadFile(dataDir string, hashlistID int64) string {
	matches, err := filepath.Glob(filepath.Join(dataDir, strconv.FormatInt(hashlistID, 10)+"_*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// handleRevalidate swaps the declared hash type on a paused hashlist and
// re-runs upload-time validation against the same source file (GitHub issue
// #38). Used when the user realises they picked the wrong type and wants to
// recover without re-uploading the file.
//
// Allowed only while the hashlist is in 'awaiting_validation_decision'.
func (h *hashlistHandler) handleRevalidate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, "Invalid hashlist id", http.StatusBadRequest)
		return
	}

	var body struct {
		HashTypeID int `json:"hash_type_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	hashlist, err := h.hashlistRepo.GetByID(ctx, id)
	if err != nil {
		jsonError(w, "Hashlist not found", http.StatusNotFound)
		return
	}
	if hashlist.Status != models.HashListStatusAwaitingValidationDecision {
		jsonError(w, "Hashlist is not awaiting validation decision (current status: "+hashlist.Status+")", http.StatusConflict)
		return
	}

	// Verify the new hash type is real and enabled.
	hashType, err := h.hashTypeRepo.GetByID(ctx, body.HashTypeID)
	if err != nil || hashType == nil || !hashType.IsEnabled {
		jsonError(w, "Invalid or disabled hash type", http.StatusBadRequest)
		return
	}

	hashlistPath := findUploadFile(h.dataDir, id)
	if hashlistPath == "" {
		jsonError(w, "Uploaded file not found on disk; cannot re-validate", http.StatusInternalServerError)
		return
	}

	// Clean slate: drop existing invalid_hashes rows and reset the validation
	// counters before running the new pass. We update the declared hash type
	// last so a mid-flight failure doesn't leave the hashlist pointing at a
	// new type with stale invalid_hashes for the old type.
	if err := h.invalidHashRepo.DeleteByHashlist(ctx, id); err != nil {
		debug.Error("Revalidate: failed to clear invalid_hashes for hashlist %d: %v", id, err)
		jsonError(w, "Failed to reset validation state", http.StatusInternalServerError)
		return
	}
	if err := h.hashlistRepo.UpdateHashType(ctx, id, body.HashTypeID); err != nil {
		debug.Error("Revalidate: failed to update hash type for hashlist %d: %v", id, err)
		jsonError(w, "Failed to update hash type", http.StatusInternalServerError)
		return
	}
	hashlist.HashTypeID = body.HashTypeID

	outcome, err := h.validationService.ValidateUpload(ctx, id, hashlistPath, body.HashTypeID)
	if err != nil {
		debug.Error("Revalidate: validation failed for hashlist %d: %v", id, err)
		jsonError(w, "Failed to re-validate hashlist", http.StatusInternalServerError)
		return
	}

	// Refresh from DB so the response carries the updated columns the
	// validator wrote (invalid_count, total_input_lines, validation_notice).
	fresh, ferr := h.hashlistRepo.GetByID(ctx, id)
	if ferr == nil && fresh != nil {
		hashlist = fresh
	}

	// Whether or not invalid lines were found, the hashlist stays paused at
	// awaiting_validation_decision — the user explicitly clicks Proceed (or
	// re-revalidates) from the dialog. This is symmetric with the original
	// upload path and avoids dispatching the processor before the user has
	// reviewed the new results.
	extras := map[string]interface{}{
		"validation_status": "awaiting_decision",
		"total_input_lines": outcome.TotalInputLines,
		"valid_count":       outcome.ValidCount,
		"invalid_count":     outcome.InvalidCount,
		"truncated":         outcome.Truncated,
		"sample_invalid":    outcome.InvalidSample,
	}
	if !outcome.HasValidator {
		// No-validator types skip the pause path: drive the hashlist forward
		// to processing immediately and return a notice instead of a preview.
		extras["validation_status"] = "no_validator"
		extras["validation_notice"] = outcome.Notice
		if err := h.hashlistRepo.UpdateStatus(ctx, id, models.HashListStatusProcessing, ""); err != nil {
			debug.Error("Revalidate: failed to advance hashlist %d to processing: %v", id, err)
			jsonError(w, "Failed to advance hashlist", http.StatusInternalServerError)
			return
		}
		go h.processor.SubmitHashlistForProcessing(id, hashlistPath)
	}

	body2, err := flattenHashlistResponse(hashlist, extras)
	if err != nil {
		debug.Error("Revalidate: failed to encode response: %v", err)
		jsonError(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body2)
}

// handleChangeHashlistHashType changes a hashlist's hash type AFTER it has
// already been processed, and recovers any jobs that failed because the previous
// type was wrong (the HASHLIST_FATAL cascade in AttributeBenchmarkFailure). This
// is the post-failure sibling of handleRevalidate (which only runs during the
// upload-time awaiting_validation_decision pause). NOTE: distinct from
// handleUpdateHashType, which is admin CRUD on hash-type *definitions*.
//
// Flow: validate the new type → re-validate the source file against it (so the
// invalid-hash counters refresh) → flip the hashlist back to ready → re-queue
// every FAILED job on the hashlist under the new type (keyspace untouched;
// --skip/--limit and --total-candidates are type-independent) → clear those
// jobs' benchmark blocklist + failure counters so they resume immediately
// instead of waiting out the 24h cooldown.
func (h *hashlistHandler) handleChangeHashlistHashType(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := getInt64FromPath(r, "id")
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	var body struct {
		HashTypeID int `json:"hash_type_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	hashlist, err := h.hashlistRepo.GetByID(ctx, id)
	if err != nil {
		jsonError(w, "Hashlist not found", http.StatusNotFound)
		return
	}

	// Verify the new hash type is real and enabled.
	hashType, err := h.hashTypeRepo.GetByID(ctx, body.HashTypeID)
	if err != nil || hashType == nil || !hashType.IsEnabled {
		jsonError(w, "Invalid or disabled hash type", http.StatusBadRequest)
		return
	}

	// Re-validate against the source file when it's still on disk so the
	// invalid-hash counters reflect the new type. A missing file is non-fatal
	// here — the hashlist is already processed; the scheduler will re-benchmark
	// and re-classify if the new type is also wrong.
	if path := findUploadFile(h.dataDir, id); path != "" {
		if err := h.invalidHashRepo.DeleteByHashlist(ctx, id); err != nil {
			debug.Warning("hash-type change: clear invalid_hashes for hashlist %d: %v", id, err)
		}
		if err := h.hashlistRepo.UpdateHashType(ctx, id, body.HashTypeID); err != nil {
			jsonError(w, "Failed to update hash type", http.StatusInternalServerError)
			return
		}
		if _, vErr := h.validationService.ValidateUpload(ctx, id, path, body.HashTypeID); vErr != nil {
			debug.Warning("hash-type change: revalidation for hashlist %d failed: %v", id, vErr)
		}
	} else if err := h.hashlistRepo.UpdateHashType(ctx, id, body.HashTypeID); err != nil {
		jsonError(w, "Failed to update hash type", http.StatusInternalServerError)
		return
	}

	// Clear the cascade error state and mark the hashlist usable again.
	if err := h.hashlistRepo.UpdateStatus(ctx, id, models.HashListStatusReady, ""); err != nil {
		debug.Warning("hash-type change: reset status for hashlist %d: %v", id, err)
	}

	// Re-queue failed jobs under the new type and clear their blocklists so the
	// scheduler re-benchmarks them immediately.
	jobExecRepo := repository.NewJobExecutionRepository(h.db)
	benchmarkRepo := repository.NewBenchmarkRepository(h.db)
	requeued, rErr := jobExecRepo.RequeueFailedJobsByHashlist(ctx, id, body.HashTypeID)
	if rErr != nil {
		debug.Error("hash-type change: requeue failed jobs for hashlist %d: %v", id, rErr)
		jsonError(w, "Hash type updated but failed to requeue jobs", http.StatusInternalServerError)
		return
	}
	for _, jid := range requeued {
		if _, cErr := benchmarkRepo.ClearBlocklistForJob(ctx, jid, userID); cErr != nil {
			debug.Warning("hash-type change: clear blocklist for job %s: %v", jid, cErr)
		}
	}
	debug.Info("hash-type change: hashlist %d -> type %d, requeued %d failed job(s)", id, body.HashTypeID, len(requeued))

	if fresh, ferr := h.hashlistRepo.GetByID(ctx, id); ferr == nil && fresh != nil {
		hashlist = fresh
	}
	resp, err := flattenHashlistResponse(hashlist, map[string]interface{}{
		"requeued_jobs": len(requeued),
		"new_hash_type": body.HashTypeID,
	})
	if err != nil {
		jsonError(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

// handleListInvalidHashes returns paginated invalid-line entries for a
// hashlist. Used by the frontend validation-preview dialog.
func (h *hashlistHandler) handleListInvalidHashes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, "Invalid hashlist id", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 50
	}

	total, err := h.invalidHashRepo.CountByHashlist(ctx, id)
	if err != nil {
		debug.Error("Failed to count invalid hashes for hashlist %d: %v", id, err)
		jsonError(w, "Failed to load invalid hashes", http.StatusInternalServerError)
		return
	}
	items, err := h.invalidHashRepo.ListByHashlist(ctx, id, pageSize, (page-1)*pageSize)
	if err != nil {
		debug.Error("Failed to list invalid hashes for hashlist %d: %v", id, err)
		jsonError(w, "Failed to load invalid hashes", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items":     items,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	})
}
