package cloud

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

/*
 * Admin API for the provisioning rules — WHEN the system may spend, as distinct
 * from the budget ladder's HOW MUCH.
 *
 * THE READ ROUTES ARE DELIBERATELY ASYMMETRIC, and conflating them is the one
 * mistake here that quietly changes policy for every client:
 *
 *   GET /policy-equivalent for rules  -> GetSystemDefault, UNMERGED.
 *      An admin editing the defaults has to see what the defaults themselves
 *      say. Handing them a merged view and saving it back would bake one
 *      client's override into the defaults for everyone.
 *
 *   GET /clients/{id}/rules           -> GetRules, MERGED.
 *      What that client is actually subject to, which is the only thing worth
 *      showing on a per-client screen. The repository stamps the requested
 *      client onto the result so a round trip through the save route below
 *      cannot target the system-default row.
 *
 * The effective view also carries `inherited` so the UI can render a field the
 * client has not overridden differently from one they set to the same value —
 * the difference matters, because the first tracks a later change to the
 * default and the second does not.
 */

// GetDefaultRules returns the system-default rules, unmerged.
func (h *Handler) GetDefaultRules(w http.ResponseWriter, r *http.Request) {
	if h.rules == nil {
		writeErr(w, http.StatusServiceUnavailable, "provisioning rules are not available")
		return
	}
	rules, err := h.rules.GetSystemDefault(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

// UpdateDefaultRules edits the system defaults.
func (h *Handler) UpdateDefaultRules(w http.ResponseWriter, r *http.Request) {
	h.saveRules(w, r, nil)
}

/*
 * GetClientRules returns the rules in force for one client, plus the raw
 * override and the system default they were merged from.
 *
 * All three are returned because the UI needs to distinguish "inherited" from
 * "set to the same value", and deriving that client-side from the effective
 * view alone is impossible.
 */
func (h *Handler) GetClientRules(w http.ResponseWriter, r *http.Request) {
	if h.rules == nil {
		writeErr(w, http.StatusServiceUnavailable, "provisioning rules are not available")
		return
	}
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}

	effective, err := h.rules.GetRules(r.Context(), clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	systemDefault, err := h.rules.GetSystemDefault(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	override, err := h.rules.GetClientOverride(r.Context(), clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"effective":      effective,
		"override":       override, // null when the client inherits everything
		"system_default": systemDefault,
	})
}

// UpdateClientRules creates or edits a client's override.
func (h *Handler) UpdateClientRules(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}
	h.saveRules(w, r, &clientID)
}

// DeleteClientRules removes a client's override so it inherits the defaults
// again. Distinct from writing an override full of nulls, which would look the
// same today and stop looking the same the moment any field gains a meaning
// for an explicit null.
func (h *Handler) DeleteClientRules(w http.ResponseWriter, r *http.Request) {
	if h.rules == nil {
		writeErr(w, http.StatusServiceUnavailable, "provisioning rules are not available")
		return
	}
	clientID, err := uuid.Parse(mux.Vars(r)["clientId"])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid client id")
		return
	}
	if err := h.rules.DeleteClientRules(r.Context(), clientID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
 * saveRules is the shared write path.
 *
 * The CLIENT ID COMES FROM THE URL, never from the body. A body-supplied
 * client_id would let a request against /clients/A/rules write B's override —
 * or, with a null, the system default for everyone. The URL is the only thing
 * the route's authorisation was checked against, so it is the only thing
 * allowed to name the target.
 */
func (h *Handler) saveRules(w http.ResponseWriter, r *http.Request, clientID *uuid.UUID) {
	if h.rules == nil {
		writeErr(w, http.StatusServiceUnavailable, "provisioning rules are not available")
		return
	}

	var in models.CloudProvisioningRules
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	in.ClientID = clientID
	in.ID = uuid.Nil // the target is the URL, not a row id in the body

	if err := h.rules.UpsertRules(r.Context(), &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Read back through the same path the provisioning gates use, so the
	// response shows what the rules now actually resolve to rather than
	// echoing the request.
	if clientID == nil {
		out, err := h.rules.GetSystemDefault(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	out, err := h.rules.GetRules(r.Context(), *clientID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
