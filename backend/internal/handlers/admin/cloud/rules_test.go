package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/*
 * Handler-level tests for the provisioning rules routes.
 *
 * The repository underneath is covered separately; what is only reachable here
 * is the ROUTING of identity — which client a write lands on. That is decided
 * by the handler alone, and getting it wrong is silent.
 */

func rulesHandler(t *testing.T) (*Handler, *db.DB) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)

	// Only the rules repository is exercised by these routes; the rest of the
	// handler's dependencies are deliberately nil so a test that strays into
	// another route fails loudly rather than half-working.
	return NewHandler(nil, nil, nil, nil, nil,
		repository.NewCloudProvisioningRulesRepository(database),
		nil, nil, nil), database
}

func serveRules(t *testing.T, h *Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	r := mux.NewRouter()
	h.RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

/*
 * TestUpdateClientRules_IgnoresABodySuppliedClientID is the security-relevant
 * one, and it fails silently in production if it regresses.
 *
 * A request authorised against /clients/A/rules must write A's override and
 * nothing else. If the body's client_id were honoured, the same request could
 * target client B — or, with an explicit null, the SYSTEM DEFAULT that governs
 * every client. The URL is the only thing the route's authorisation was checked
 * against, so it has to be the only thing that names the target.
 */
func TestUpdateClientRules_IgnoresABodySuppliedClientID(t *testing.T) {
	h, database := rulesHandler(t)
	ctx := context.Background()
	repo := repository.NewCloudProvisioningRulesRepository(database)

	victim := testutil.CreateTestClient(t, database, "victim-"+uuid.NewString()[:8], testutil.ClientCloudOpts{})
	target := testutil.CreateTestClient(t, database, "target-"+uuid.NewString()[:8], testutil.ClientCloudOpts{})

	// A write against `target`, whose body names `victim`.
	rec := serveRules(t, h, http.MethodPut, "/cloud/clients/"+target.String()+"/rules",
		map[string]interface{}{
			"client_id":        victim.String(),
			"min_job_priority": 800,
		})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The victim must be untouched — no override row at all.
	victimOverride, err := repo.GetClientOverride(ctx, victim)
	require.NoError(t, err)
	assert.Nil(t, victimOverride,
		"a body-supplied client_id wrote another client's override; the URL must name the target")

	// The write landed on the client named in the URL.
	targetOverride, err := repo.GetClientOverride(ctx, target)
	require.NoError(t, err)
	require.NotNil(t, targetOverride, "the write should have created an override for the URL's client")
	require.NotNil(t, targetOverride.MinJobPriority)
	assert.Equal(t, 800, *targetOverride.MinJobPriority)
}

/*
 * TestUpdateClientRules_CannotReachTheSystemDefault: the same attack with a
 * NULL client_id, which is the more damaging shape — the system default governs
 * every client that has not overridden a field.
 */
func TestUpdateClientRules_CannotReachTheSystemDefault(t *testing.T) {
	h, database := rulesHandler(t)
	ctx := context.Background()
	repo := repository.NewCloudProvisioningRulesRepository(database)

	client := testutil.CreateTestClient(t, database, "c-"+uuid.NewString()[:8], testutil.ClientCloudOpts{})

	rec := serveRules(t, h, http.MethodPut, "/cloud/clients/"+client.String()+"/rules",
		map[string]interface{}{
			"client_id":        nil,
			"min_job_priority": 950,
		})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	def, err := repo.GetSystemDefault(ctx)
	require.NoError(t, err)
	require.NotNil(t, def.MinJobPriority)
	assert.Equal(t, 0, *def.MinJobPriority,
		"a null client_id in the body rewrote the system default for every client")

	var defaults int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_provisioning_rules WHERE client_id IS NULL`).Scan(&defaults))
	assert.Equal(t, 1, defaults, "there must be exactly one system-default row")
}

/*
 * TestGetClientRules_ReturnsEffectiveOverrideAndDefault.
 *
 * All three are returned because `effective` alone cannot distinguish
 * "inherited" from "set to the same value as the default", and the two behave
 * differently: only the inherited one follows a later change to the default. A
 * UI that cannot tell them apart converts inheritance into a pinned copy the
 * first time anyone saves the form.
 */
func TestGetClientRules_ReturnsEffectiveOverrideAndDefault(t *testing.T) {
	h, database := rulesHandler(t)
	client := testutil.CreateTestClient(t, database, "c-"+uuid.NewString()[:8], testutil.ClientCloudOpts{})

	// No override yet: effective mirrors the default, and override is null.
	rec := serveRules(t, h, http.MethodGet, "/cloud/clients/"+client.String()+"/rules", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var view struct {
		Effective     *models.CloudProvisioningRules `json:"effective"`
		Override      *models.CloudProvisioningRules `json:"override"`
		SystemDefault *models.CloudProvisioningRules `json:"system_default"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))

	require.NotNil(t, view.Effective)
	require.NotNil(t, view.SystemDefault)
	assert.Nil(t, view.Override, "a client that inherits everything has no override row")
	require.NotNil(t, view.Effective.MinStarvationSeconds)
	assert.Equal(t, 180, *view.Effective.MinStarvationSeconds)

	// The effective view must carry the client it describes, so the obvious
	// load-edit-save round trip cannot target the system-default row.
	require.NotNil(t, view.Effective.ClientID)
	assert.Equal(t, client, *view.Effective.ClientID)
}

/*
 * TestGetDefaultRules_IsUnmerged pins the asymmetry between the two read routes.
 *
 * /rules must return the default ITSELF, not a merged view. Serving a merged
 * result to the defaults screen and saving it back would bake one client's
 * override into the defaults for everyone.
 */
func TestGetDefaultRules_IsUnmerged(t *testing.T) {
	h, database := rulesHandler(t)
	client := testutil.CreateTestClient(t, database, "c-"+uuid.NewString()[:8], testutil.ClientCloudOpts{})

	// Give the client a distinctive override.
	rec := serveRules(t, h, http.MethodPut, "/cloud/clients/"+client.String()+"/rules",
		map[string]interface{}{"min_starvation_seconds": 4242})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = serveRules(t, h, http.MethodGet, "/cloud/rules", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var def models.CloudProvisioningRules
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &def))

	assert.Nil(t, def.ClientID, "the defaults route must return the system-default row itself")
	require.NotNil(t, def.MinStarvationSeconds)
	assert.Equal(t, 180, *def.MinStarvationSeconds,
		"a client override leaked into the system-default view")
}

// TestDeleteClientRules_RestoresInheritance: dropping an override is distinct
// from writing one full of nulls, and must leave the client on the defaults.
func TestDeleteClientRules_RestoresInheritance(t *testing.T) {
	h, database := rulesHandler(t)
	ctx := context.Background()
	repo := repository.NewCloudProvisioningRulesRepository(database)
	client := testutil.CreateTestClient(t, database, "c-"+uuid.NewString()[:8], testutil.ClientCloudOpts{})

	rec := serveRules(t, h, http.MethodPut, "/cloud/clients/"+client.String()+"/rules",
		map[string]interface{}{"min_starvation_seconds": 30})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = serveRules(t, h, http.MethodDelete, "/cloud/clients/"+client.String()+"/rules", nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	override, err := repo.GetClientOverride(ctx, client)
	require.NoError(t, err)
	assert.Nil(t, override, "the override row should be gone")

	effective, err := repo.GetRules(ctx, client)
	require.NoError(t, err)
	require.NotNil(t, effective.MinStarvationSeconds)
	assert.Equal(t, 180, *effective.MinStarvationSeconds, "the client should be back on the defaults")
}
