package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

/*
 * There is no funded RunPod account behind this adapter, and community testers
 * will be the first to point real money at it. These tests are therefore the
 * ONLY verification it gets before that happens, which is why the base URLs are
 * struct fields: vastai.go spent its whole life untestable because its endpoint
 * was a package constant, and RunPod could not afford to repeat that.
 *
 * What is covered here is everything that does not need an account: the traps
 * that cost money when they are wrong. What is NOT covered is listed in the
 * plan's twelve-item checklist and in the provider docs.
 */

type runpodFake struct {
	pods []runpodPod
	// createStatus overrides the response to POST /pods.
	createStatus int
	createBody   string
	deleteStatus int
	deleteBody   string
	gqlBody      string
	created      []map[string]interface{}
}

func newRunPod(t *testing.T, kind models.CloudProvider, fake *runpodFake, s RunPodSettings) *RunPodProvider {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/pods":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			fake.created = append(fake.created, body)
			if fake.createStatus != 0 && fake.createStatus != http.StatusOK {
				w.WriteHeader(fake.createStatus)
				_, _ = w.Write([]byte(fake.createBody))
				return
			}
			_ = json.NewEncoder(w).Encode(runpodPod{
				ID: "pod-new", Name: body["name"].(string), DesiredStatus: "RUNNING", CostPerHr: 0.34,
			})

		case r.Method == http.MethodGet && r.URL.Path == "/pods":
			out := fake.pods
			if name := r.URL.Query().Get("name"); name != "" {
				var filtered []runpodPod
				for _, p := range out {
					if p.Name == name {
						filtered = append(filtered, p)
					}
				}
				out = filtered
			}
			_ = json.NewEncoder(w).Encode(out)

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/pods/"):
			if fake.deleteStatus != 0 {
				w.WriteHeader(fake.deleteStatus)
				_, _ = w.Write([]byte(fake.deleteBody))
				return
			}
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/pods/"):
			id := strings.TrimPrefix(r.URL.Path, "/pods/")
			for _, p := range fake.pods {
				if p.ID == id {
					_ = json.NewEncoder(w).Encode(p)
					return
				}
			}
			http.NotFound(w, r)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	gql := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := fake.gqlBody
		if body == "" {
			body = `{"data":{"gpuTypes":[]}}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(gql.Close)

	p, err := NewRunPodProvider(RunPodCredentials{APIKey: "k"}, kind, s)
	if err != nil {
		t.Fatalf("NewRunPodProvider: %v", err)
	}
	p.RESTBaseURL = srv.URL
	p.GraphQLURL = gql.URL
	p.MinInterval = 0
	return p
}

func TestBothTiersBuildFromTheSameAdapter(t *testing.T) {
	for _, kind := range []models.CloudProvider{
		models.CloudProviderRunPod, models.CloudProviderRunPodCommunity,
	} {
		p := newRunPod(t, kind, &runpodFake{}, RunPodSettings{})
		if p.Name() != kind {
			t.Errorf("Name() = %s, want %s. The two tiers must never both render as "+
				"\"RunPod\": an admin acknowledging third-party exposure needs to see it is "+
				"the Community tier they are consenting to.", p.Name(), kind)
		}
	}

	secure := newRunPod(t, models.CloudProviderRunPod, &runpodFake{}, RunPodSettings{})
	community := newRunPod(t, models.CloudProviderRunPodCommunity, &runpodFake{}, RunPodSettings{})
	if secure.cloudType() != "SECURE" || community.cloudType() != "COMMUNITY" {
		t.Errorf("cloudType mismatch: %s / %s", secure.cloudType(), community.cloudType())
	}
	if _, err := NewRunPodProvider(RunPodCredentials{APIKey: "k"}, models.CloudProviderAWS, RunPodSettings{}); err == nil {
		t.Error("the constructor accepted a non-RunPod kind")
	}
}

/*
 * The single most dangerous line in the adapter.
 *
 * offerVerified drops an offer only when Raw["verified"] is present AND false.
 * The caller hardcodes VerifiedOnly, so setting the key to false on Community
 * "for symmetry" would delete every Community offer at the filter stage — the
 * tier would be configurable, enablable, allowlistable and would never once
 * produce a candidate, with no error anywhere.
 */
func TestCommunityLeavesVerifiedAbsentSoItsOffersSurvive(t *testing.T) {
	community := newRunPod(t, models.CloudProviderRunPodCommunity, &runpodFake{}, RunPodSettings{})
	raw := community.offerRaw(runpodGPUType{ID: "NVIDIA GeForce RTX 4090"}, &runpodLowestPrice{}, "")
	if _, present := raw["verified"]; present {
		t.Fatalf("Community set Raw[\"verified\"] = %v; it must be ABSENT.\n"+
			"Absent reads as unknown and passes VerifiedOnly. Present-and-false is a "+
			"hard drop, and the caller sets VerifiedOnly unconditionally — so this one "+
			"key decides whether runpod_community can ever rent anything at all.",
			raw["verified"])
	}

	secure := newRunPod(t, models.CloudProviderRunPod, &runpodFake{}, RunPodSettings{})
	if v := secure.offerRaw(runpodGPUType{ID: "x"}, &runpodLowestPrice{}, "")["verified"]; v != true {
		t.Errorf("Secure should declare verified=true, got %v", v)
	}

	// And prove it end to end through the real filter.
	offers := applyOfferConstraints([]Offer{{
		ID: "c", GPUModel: "RTX 4090", HourlyRateCents: 34,
		Raw: community.offerRaw(runpodGPUType{ID: "x"}, &runpodLowestPrice{}, ""),
	}}, OfferQuery{VerifiedOnly: true})
	if len(offers) != 1 {
		t.Error("a Community offer was deleted by VerifiedOnly")
	}
}

func TestLaunchAdoptsAnExistingPodRatherThanPayingTwice(t *testing.T) {
	label := "kh-abcdef01-2345-6789"
	fake := &runpodFake{pods: []runpodPod{{ID: "pod-existing", Name: label, DesiredStatus: "RUNNING"}}}
	p := newRunPod(t, models.CloudProviderRunPod, fake, RunPodSettings{})

	res, err := p.Launch(context.Background(), LaunchRequest{
		Label: label, Offer: Offer{InstanceType: "NVIDIA GeForce RTX 4090"}, DiskGB: 40,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if res.ProviderInstanceID != "pod-existing" {
		t.Errorf("adopted %q, want pod-existing", res.ProviderInstanceID)
	}
	if len(fake.created) != 0 {
		t.Error("a second pod was CREATED despite one already carrying the label.\n" +
			"POST /pods has no idempotency key, so the label is the only one there is. " +
			"Skipping the adopt check means a retried launch pays for two GPUs.")
	}
}

/*
 * An ambiguous failure must NOT become ErrOfferUnavailable.
 *
 * attemptLaunch treats that sentinel as "nothing was created, nothing is
 * billing" and RELEASES THE RESERVATION. A 5xx or a timeout may well have
 * created a pod, so classifying it that way frees the budget for a GPU that is
 * quietly running.
 */
func TestAmbiguousCreateFailureIsNotReportedAsUnavailableCapacity(t *testing.T) {
	fake := &runpodFake{createStatus: http.StatusBadGateway, createBody: "upstream boom"}
	p := newRunPod(t, models.CloudProviderRunPod, fake, RunPodSettings{})

	_, err := p.Launch(context.Background(), LaunchRequest{
		Label: "kh-abcdef01-2345-6789", Offer: Offer{InstanceType: "gpu"},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if isErrOfferUnavailable(err) {
		t.Error("a 5xx was classified as ErrOfferUnavailable.\n" +
			"That sentinel releases the reservation. A 5xx may have created a pod, so " +
			"this frees the budget while a GPU keeps billing — and the row is gone, so " +
			"nothing is left to reconcile it against.")
	}
}

func TestGenuineCapacityRefusalIsRetryable(t *testing.T) {
	fake := &runpodFake{
		createStatus: http.StatusBadRequest,
		createBody:   `{"error":"There are no instances available with the requested specifications"}`,
	}
	p := newRunPod(t, models.CloudProviderRunPod, fake, RunPodSettings{})

	_, err := p.Launch(context.Background(), LaunchRequest{
		Label: "kh-abcdef01-2345-6789", Offer: Offer{InstanceType: "gpu"},
	})
	if !isErrOfferUnavailable(err) {
		t.Errorf("a real capacity refusal was not retryable: %v.\n"+
			"Without the sentinel the launch walk aborts instead of trying the next "+
			"candidate, and the reservation is held for a pod that was never created.", err)
	}
}

/*
 * Two pods, one label: the expected outcome of a double launch, and the case a
 * plain map write silently loses.
 */
func TestDuplicateLabelsAreBothSurfaced(t *testing.T) {
	label := "kh-abcdef01-2345-6789"
	fake := &runpodFake{pods: []runpodPod{
		{ID: "pod-a", Name: label, DesiredStatus: "RUNNING", LastStartedAt: "2026-01-01T00:00:00Z"},
		{ID: "pod-b", Name: label, DesiredStatus: "RUNNING", LastStartedAt: "2026-01-01T00:05:00Z"},
	}}
	p := newRunPod(t, models.CloudProviderRunPod, fake, RunPodSettings{})

	owned, err := p.ListOwned(context.Background())
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	if len(owned) != 2 {
		t.Fatalf("got %d entries, want 2.\n"+
			"Both pods are billing. A plain map assignment keeps one and DROPS the "+
			"other, and the dropped pod never appears in any inventory again — "+
			"invisible, unowned and billing until someone notices by hand.", len(owned))
	}
	if _, ok := owned[label]; !ok {
		t.Error("the canonical label is missing")
	}
	var dupKey string
	for k := range owned {
		if strings.Contains(k, "#dup:") {
			dupKey = k
		}
	}
	if dupKey == "" {
		t.Fatal("the duplicate was not re-keyed under a synthetic label")
	}
	// Stability matters: the orphan grace timer resets if the key moves, and a
	// grace period that never elapses is a pod that is never reaped.
	again, _ := p.ListOwned(context.Background())
	if _, ok := again[dupKey]; !ok {
		t.Errorf("the synthetic key changed between sweeps (%q vanished).\n"+
			"orphanFirstSeen keys off it, so a moving key restarts the grace period "+
			"every sweep and the duplicate is never actually reaped.", dupKey)
	}
}

func TestListOwnedIgnoresPodsThatAreNotOurs(t *testing.T) {
	fake := &runpodFake{pods: []runpodPod{
		{ID: "1", Name: "kh-abcdef01-2345-6789", DesiredStatus: "RUNNING"},
		{ID: "2", Name: "someone-elses-training-run", DesiredStatus: "RUNNING"},
		// The reason the regex is anchored: this must NOT be adopted, because
		// adoption here means eligibility for automatic destruction.
		{ID: "3", Name: "my-kh-abcdef01-2345-6789-backup", DesiredStatus: "RUNNING"},
	}}
	p := newRunPod(t, models.CloudProviderRunPod, fake, RunPodSettings{})

	owned, err := p.ListOwned(context.Background())
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	if len(owned) != 1 {
		t.Errorf("adopted %d pods, want 1. RunPod has no ownership tag — only a name — "+
			"so an unanchored match would put someone else's pod on the destruction list.",
			len(owned))
	}
}

func TestDesiredStatusMapping(t *testing.T) {
	cases := []struct {
		desired  string
		state    InstanceObservedState
		terminal bool
	}{
		{"RUNNING", ObservedRunning, false},
		{"running", ObservedRunning, false},
		// Terminal on purpose: a stopped pod bills container disk at roughly
		// double, so "recoverable" and "destroy it" are both true.
		{"EXITED", ObservedStopped, true},
		{"TERMINATED", ObservedGone, true},
		{"", ObservedPending, false},
		{"SOMETHING_NEW", ObservedPending, false},
	}
	for _, c := range cases {
		state, terminal := normalizeRunPodState(c.desired)
		if state != c.state || terminal != c.terminal {
			t.Errorf("%q -> (%v,%v), want (%v,%v)", c.desired, state, terminal, c.state, c.terminal)
		}
	}
}

/*
 * ObservedGone is Terminal, and Terminal triggers destroy-and-finalize. Mapping
 * a transient server error to it would finalize the row for a pod that is still
 * running and still billing.
 */
func TestStatusDoesNotTreatAServerErrorAsGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p, _ := NewRunPodProvider(RunPodCredentials{APIKey: "k"}, models.CloudProviderRunPod, RunPodSettings{})
	p.RESTBaseURL = srv.URL
	p.MinInterval = 0

	st, err := p.Status(context.Background(), "pod-x")
	if err == nil && st != nil && st.State == ObservedGone {
		t.Fatal("a 500 was reported as ObservedGone.\n" +
			"Gone is Terminal, and Terminal finalizes the instance row and releases its " +
			"budget — for a pod that is still running and still billing.")
	}
	if err == nil {
		t.Error("expected the server error to surface")
	}
}

func TestStatusTreats404AsGone(t *testing.T) {
	p := newRunPod(t, models.CloudProviderRunPod, &runpodFake{}, RunPodSettings{})
	st, err := p.Status(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != ObservedGone || !st.Terminal {
		t.Errorf("a 404 gave (%v, terminal=%v), want Gone+terminal", st.State, st.Terminal)
	}
}

func TestDestroyIsIdempotentAcrossEveryPlausibleNotFoundShape(t *testing.T) {
	// RunPod's response to deleting a nonexistent pod could not be verified
	// without an account, so all three plausible shapes must succeed.
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"200", http.StatusOK, ""},
		{"404", http.StatusNotFound, ""},
		{"400 with a message", http.StatusBadRequest, `{"error":"pod not found"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newRunPod(t, models.CloudProviderRunPod,
				&runpodFake{deleteStatus: c.status, deleteBody: c.body}, RunPodSettings{})
			if err := p.Destroy(context.Background(), "pod-x"); err != nil {
				t.Errorf("Destroy returned %v.\n"+
					"Already-gone is success: the contract requires convergence, and a "+
					"reaper that never converges retries forever against a pod that does "+
					"not exist.", err)
			}
		})
	}
}

/*
 * A blank id is NOT "already gone".
 *
 * Returning nil there would finalize the database row for a pod that is still
 * billing. Erroring keeps the row alive so the reaper reconciles by label.
 */
func TestDestroyRefusesABlankID(t *testing.T) {
	p := newRunPod(t, models.CloudProviderRunPod, &runpodFake{}, RunPodSettings{})
	if err := p.Destroy(context.Background(), ""); err == nil {
		t.Error("Destroy(\"\") returned success, which finalizes the row for a pod " +
			"nobody can now find — and it keeps billing")
	}
}

func TestDestroySurfacesRealFailures(t *testing.T) {
	p := newRunPod(t, models.CloudProviderRunPod,
		&runpodFake{deleteStatus: http.StatusInternalServerError, deleteBody: "boom"}, RunPodSettings{})
	if err := p.Destroy(context.Background(), "pod-x"); err == nil {
		t.Error("a 500 on delete was swallowed as success; the instance is still billing")
	}
}

/*
 * The one credential decision on this provider, enforced at CONSTRUCTION so no
 * later injection site can bypass it.
 */
func TestCommunityNeverHoldsASelfDestructKey(t *testing.T) {
	creds := RunPodCredentials{APIKey: "provision-key", SelfDestructAPIKey: "narrow-key"}
	s := RunPodSettings{AllowInGuestSelfDestruct: true}

	community, err := NewRunPodProvider(creds, models.CloudProviderRunPodCommunity, s)
	if err != nil {
		t.Fatalf("NewRunPodProvider: %v", err)
	}
	if community.SelfDestructAPIKey != "" {
		t.Fatal("the Community tier is holding a self-destruct key.\n" +
			"RunPod issues no per-pod scoped credential, so the only usable key is " +
			"account-scoped — it can delete every other pod on the account. A Community " +
			"host operator has root over the container and reads it from the environment.")
	}

	secure, err := NewRunPodProvider(creds, models.CloudProviderRunPod, s)
	if err != nil {
		t.Fatalf("NewRunPodProvider: %v", err)
	}
	if secure.SelfDestructAPIKey != "narrow-key" {
		t.Errorf("Secure should carry the NARROW key, got %q", secure.SelfDestructAPIKey)
	}
}

func TestSelfDestructKeyIsNeverPutInACommunityPodsEnvironment(t *testing.T) {
	fake := &runpodFake{}
	p := newRunPod(t, models.CloudProviderRunPodCommunity, fake,
		RunPodSettings{AllowInGuestSelfDestruct: true})
	p.SelfDestructAPIKey = "" // constructor already refused; assert the launch path too

	if _, err := p.Launch(context.Background(), LaunchRequest{
		Label: "kh-abcdef01-2345-6789", Offer: Offer{InstanceType: "gpu"},
		Env: map[string]string{"KH_HOST": "x"},
	}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	env, _ := fake.created[0]["env"].(map[string]interface{})
	if _, present := env["KH_RUNPOD_API_KEY"]; present {
		t.Error("an account-scoped RunPod key was placed in a Community pod's environment")
	}
}

/*
 * A network volume outlives the pod, so one would retain cracked plaintexts
 * after termination. Not configurable, and asserted so it stays that way.
 */
func TestLaunchNeverAttachesANetworkVolume(t *testing.T) {
	fake := &runpodFake{}
	p := newRunPod(t, models.CloudProviderRunPod, fake, RunPodSettings{ContainerDiskGB: 60})

	if _, err := p.Launch(context.Background(), LaunchRequest{
		Label: "kh-abcdef01-2345-6789", Offer: Offer{InstanceType: "gpu"}, DiskGB: 40,
	}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	body := fake.created[0]
	if v, _ := body["volumeInGb"].(float64); v != 0 {
		t.Errorf("volumeInGb = %v, want 0.\n"+
			"A network volume survives pod termination and would keep cracked "+
			"plaintexts on RunPod's storage after the job is over.", v)
	}
	// The floor is a floor, not an override.
	if d, _ := body["containerDiskInGb"].(float64); d != 60 {
		t.Errorf("containerDiskInGb = %v, want the larger of the job's 40 and the "+
			"configured floor of 60", d)
	}
}

func TestLaunchSendsTheTierAndThePlacement(t *testing.T) {
	fake := &runpodFake{}
	p := newRunPod(t, models.CloudProviderRunPodCommunity, fake, RunPodSettings{})

	if _, err := p.Launch(context.Background(), LaunchRequest{
		Label: "kh-abcdef01-2345-6789",
		Offer: Offer{InstanceType: "NVIDIA GeForce RTX 4090", PlacementRef: "EU-RO-1"},
	}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	body := fake.created[0]
	if body["cloudType"] != "COMMUNITY" {
		t.Errorf("cloudType = %v, want COMMUNITY. Sending the wrong tier puts client "+
			"hashes on peer hardware under a config that was acknowledged as Secure.",
			body["cloudType"])
	}
	dcs, _ := body["dataCenterIds"].([]interface{})
	if len(dcs) != 1 || dcs[0] != "EU-RO-1" {
		t.Errorf("dataCenterIds = %v, want [EU-RO-1]", body["dataCenterIds"])
	}
	// The RAW gpuType id must round-trip: displayName would not be accepted.
	ids, _ := body["gpuTypeIds"].([]interface{})
	if len(ids) != 1 || ids[0] != "NVIDIA GeForce RTX 4090" {
		t.Errorf("gpuTypeIds = %v", body["gpuTypeIds"])
	}
}

func TestCostSoFarDeclinesToGuess(t *testing.T) {
	p := newRunPod(t, models.CloudProviderRunPod, &runpodFake{}, RunPodSettings{})
	cents, authoritative, err := p.CostSoFar(context.Background(), "pod-x")
	if err != nil || cents != 0 || authoritative {
		t.Errorf("CostSoFar = (%d, %v, %v), want (0, false, nil).\n"+
			"costPerHr x elapsed IS the caller's own wall-clock estimate, so returning it "+
			"as authoritative launders an estimate into a fact and stops the caller "+
			"correcting it. It is also wrong under RunPod's one-hour minimum bucket: at "+
			"minute five the real charge is a full hour.", cents, authoritative, err)
	}
}

func TestParseRunPodCredentialsAcceptsBothShapes(t *testing.T) {
	if c := parseRunPodCredentials("plain-token"); c.APIKey != "plain-token" {
		t.Errorf("bare string not accepted: %+v", c)
	}
	c := parseRunPodCredentials(`{"api_key":"a","self_destruct_api_key":"b"}`)
	if c.APIKey != "a" || c.SelfDestructAPIKey != "b" {
		t.Errorf("object form mis-parsed: %+v", c)
	}
}

// isErrOfferUnavailable is a local helper; errors.Is is what production uses.
func isErrOfferUnavailable(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrOfferUnavailable.Error())
}
