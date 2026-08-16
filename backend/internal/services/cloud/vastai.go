package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

const vastBaseURL = "https://console.vast.ai"

/*
 * VastAIProvider rents GPUs from the Vast.ai marketplace.
 *
 * Three properties of this provider drive the whole design and are worth
 * stating where the code lives:
 *
 * 1. NO IDEMPOTENCY TOKEN. `PUT /api/v0/asks/{id}/` has none, so a retried
 *    request creates a SECOND paid contract. The instance label is therefore
 *    the idempotency key: list by label, adopt if present, only then create.
 *
 * 2. NO TTL, NO AUTO-DESTROY. Nothing provider-side will ever stop an
 *    instance. The in-guest watchdog and our reaper are the only teardown.
 *    Storage bills from contract creation and keeps billing while `stopped` —
 *    only DELETE stops it, so we never stop, we always destroy.
 *
 * 3. UNTRUSTED HOSTS. Instances run in unprivileged containers on
 *    individually-owned machines whose operators have root. That is why the
 *    VPN runs in userspace mode, why the file sync is job-scoped, and why
 *    enabling this provider for a client requires explicit acknowledgement.
 */
type VastAIProvider struct {
	APIKey string
	Client *http.Client

	// rateMu serialises calls: Vast.ai enforces a minimum interval between
	// requests per endpoint and returns 429 with NO Retry-After header, so the
	// backoff has to be ours.
	rateMu   sync.Mutex
	lastCall time.Time
	// MinInterval is the floor between any two API calls.
	MinInterval time.Duration
}

// NewVastAIProvider creates a Vast.ai provider.
func NewVastAIProvider(apiKey string) *VastAIProvider {
	return &VastAIProvider{
		APIKey:      apiKey,
		Client:      &http.Client{Timeout: 60 * time.Second},
		MinInterval: 3 * time.Second,
	}
}

// Name identifies the provider.
func (v *VastAIProvider) Name() models.CloudProvider { return models.CloudProviderVastAI }

// do performs a rate-limited, authenticated API call.
func (v *VastAIProvider) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	v.rateMu.Lock()
	if wait := v.MinInterval - time.Since(v.lastCall); wait > 0 {
		select {
		case <-ctx.Done():
			v.rateMu.Unlock()
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	v.lastCall = time.Now()
	v.rateMu.Unlock()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("vastai: marshal request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, vastBaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("vastai: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := v.Client.Do(req)
	if err != nil {
		return fmt.Errorf("vastai: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("vastai: read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("vastai: rate limited: %s", strings.TrimSpace(string(payload)))
	case resp.StatusCode >= 300:
		return fmt.Errorf("vastai: %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("vastai: decode response: %w", err)
		}
	}
	return nil
}

// Preflight verifies the API key and reports the account balance, which on
// Vast.ai is the de-facto hard spending cap: at zero balance instances are
// stopped automatically.
func (v *VastAIProvider) Preflight(ctx context.Context) (*PreflightReport, error) {
	r := &PreflightReport{QuotaSource: "vast.ai prepaid balance"}

	var user struct {
		ID      int     `json:"id"`
		Email   string  `json:"email"`
		Balance float64 `json:"balance"`
	}
	if err := v.do(ctx, http.MethodGet, "/api/v0/users/current/", nil, &user); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("could not read account: %v", err))
		return r, nil
	}

	r.Identity = user.Email
	r.QuotaLimit = user.Balance
	r.OK = true

	if user.Balance <= 0 {
		r.OK = false
		r.Errors = append(r.Errors, "account balance is zero; Vast.ai will not start instances")
	} else if user.Balance < 5 {
		r.Warnings = append(r.Warnings, fmt.Sprintf("account balance is low ($%.2f)", user.Balance))
	}

	r.Warnings = append(r.Warnings,
		"Vast.ai hosts are individually-owned machines whose operators have root over the container; "+
			"hashes, wordlists and potfiles placed there are exposed to a third party")
	return r, nil
}

// vastOffer is the subset of the offer object we rely on.
type vastOffer struct {
	ID              int     `json:"id"`
	AskContractID   int     `json:"ask_contract_id"`
	GPUName         string  `json:"gpu_name"`
	NumGPUs         int     `json:"num_gpus"`
	DPHTotal        float64 `json:"dph_total"`
	StorageCost     float64 `json:"storage_cost"`
	InetDownCost    float64 `json:"inet_down_cost"`
	DiskSpace       float64 `json:"disk_space"`
	Duration        float64 `json:"duration"`
	Reliability     float64 `json:"reliability"`
	CudaMaxGood     float64 `json:"cuda_max_good"`
	Geolocation     string  `json:"geolocation"`
	Verified        bool    `json:"verified"`
	Rentable        bool    `json:"rentable"`
	InetDown        float64 `json:"inet_down"`
	DirectPortCount int     `json:"direct_port_count"`
}

// SearchOffers queries the marketplace.
//
// Filter values follow the REST API's units, which differ from the CLI's:
// `duration` is SECONDS here (the CLI multiplies by 86400) and gpu_ram would
// be MB. Getting these wrong silently returns the wrong machines.
func (v *VastAIProvider) SearchOffers(ctx context.Context, q OfferQuery) ([]Offer, error) {
	minGPUs := q.MinGPUCount
	if minGPUs < 1 {
		minGPUs = 1
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}

	query := map[string]interface{}{
		// Defaults the CLI applies implicitly. Omitting them returns
		// unrentable and already-rented machines.
		"external": map[string]interface{}{"eq": false},
		"rentable": map[string]interface{}{"eq": true},
		"rented":   map[string]interface{}{"eq": false},
		"num_gpus": map[string]interface{}{"gte": minGPUs},
		"type":     "ondemand",
		"order":    [][]string{{"dph_total", "asc"}},
		"limit":    limit,
		// Verified datacenter hosts only, unconditionally. This provider puts
		// client hash material on third-party machines, so the cheaper
		// unverified tier — which is also where the "stuck connecting" and
		// "bad driver" reports concentrate — is not offered at all.
		"verified":   map[string]interface{}{"eq": true},
		"datacenter": map[string]interface{}{"eq": true},
	}
	if q.MaxHourlyRateCents > 0 {
		query["dph_total"] = map[string]interface{}{"lt": float64(q.MaxHourlyRateCents) / 100.0}
	}
	if q.MinDiskGB > 0 {
		query["disk_space"] = map[string]interface{}{"gte": q.MinDiskGB}
		// allocated_storage folds disk into the quoted dph_total; without it
		// the returned price understates the real hourly cost.
		query["allocated_storage"] = q.MinDiskGB
	}
	if q.MinDuration > 0 {
		query["duration"] = map[string]interface{}{"gte": int(q.MinDuration.Seconds())}
	}

	var resp struct {
		Offers []vastOffer `json:"offers"`
	}
	if err := v.do(ctx, http.MethodPost, "/api/v0/bundles/", query, &resp); err != nil {
		return nil, err
	}

	offers := make([]Offer, 0, len(resp.Offers))
	for _, o := range resp.Offers {
		// The server ignores the `rented` filter; the CLI post-filters it
		// client-side and so must we.
		if !o.Rentable {
			continue
		}
		offers = append(offers, Offer{
			ID:                  fmt.Sprintf("%d", o.ID),
			InstanceType:        o.GPUName,
			GPUModel:            o.GPUName,
			GPUCount:            o.NumGPUs,
			HourlyRateCents:     int(o.DPHTotal * 100),
			StorageCentsPerHour: int(o.StorageCost * 100),
			BandwidthCentsPerGB: int(o.InetDownCost * 100),
			Region:              o.Geolocation,
			MaxDuration:         time.Duration(o.Duration) * time.Second,
			Raw: models.JSONMap{
				"reliability": o.Reliability,
				"cuda":        o.CudaMaxGood,
				"verified":    o.Verified,
				"inet_down":   o.InetDown,
			},
		})
	}
	return offers, nil
}

/*
 * Launch rents an instance.
 *
 * The label is set BEFORE anything else can go wrong, because it is the only
 * way to find this instance again if the response is lost. The returned
 * identifier is `new_contract`, NOT `id` — a detail that silently breaks
 * teardown if mishandled.
 */
func (v *VastAIProvider) Launch(ctx context.Context, req LaunchRequest) (*LaunchResult, error) {
	// Idempotency: adopt an existing instance with this label rather than
	// creating a duplicate paid contract.
	if existing, err := v.findByLabel(ctx, req.Label); err == nil && existing != "" {
		debug.Warning("vastai: label %s already exists (%s); adopting instead of creating a duplicate", req.Label, existing)
		now := time.Now()
		return &LaunchResult{ProviderInstanceID: existing, LaunchedAt: now, BilledFrom: now,
			Raw: models.JSONMap{"adopted": true}}, nil
	}

	// env must be a JSON OBJECT on this endpoint. The OpenAPI schema declares
	// it as a string, which is correct for template creation and wrong here;
	// sending a string silently drops every variable.
	env := make(map[string]string, len(req.Env))
	for k, val := range req.Env {
		env[k] = val
	}

	body := map[string]interface{}{
		"image":   req.Image,
		"disk":    req.DiskGB,
		"label":   req.Label,
		"env":     env,
		"onstart": "",
		// runtype "args" preserves the image's own ENTRYPOINT and provisions
		// no ports. Our agent dials out only, so it needs none.
		"runtype":        "args",
		"target_state":   "running",
		"cancel_unavail": true,
		"client_id":      "me",
	}

	var resp struct {
		Success     bool `json:"success"`
		NewContract int  `json:"new_contract"`
	}
	if err := v.do(ctx, http.MethodPut, "/api/v0/asks/"+req.Offer.ID+"/", body, &resp); err != nil {
		if err == ErrNotFound {
			return nil, ErrOfferUnavailable
		}
		return nil, err
	}
	if resp.NewContract == 0 {
		return nil, fmt.Errorf("vastai: launch returned no contract id")
	}

	now := time.Now()
	return &LaunchResult{
		ProviderInstanceID: fmt.Sprintf("%d", resp.NewContract),
		LaunchedAt:         now,
		// Vast.ai bills storage from contract creation, not from the moment
		// the container becomes ready, so billing starts now.
		BilledFrom: now,
		Raw:        models.JSONMap{"new_contract": resp.NewContract, "label": req.Label},
	}, nil
}

// findByLabel returns the contract id for a label, or "" if absent.
func (v *VastAIProvider) findByLabel(ctx context.Context, label string) (string, error) {
	owned, err := v.ListOwned(ctx)
	if err != nil {
		return "", err
	}
	if st, ok := owned[label]; ok {
		return st.ProviderInstanceID, nil
	}
	return "", nil
}

type vastInstance struct {
	ID           int    `json:"id"`
	ActualStatus string `json:"actual_status"`
	Label        string `json:"label"`
	StatusMsg    string `json:"status_msg"`
}

// normalizeVastState maps Vast.ai status to our vocabulary.
//
// exited, unknown and offline NEVER recover: Vast.ai's own API docs say an
// instance in those states will not reach running. Treating them as terminal
// and destroying immediately is the difference between a wasted minute and a
// wasted rental.
func normalizeVastState(status string) (InstanceObservedState, bool) {
	switch strings.ToLower(status) {
	case "running":
		return ObservedRunning, false
	case "created", "loading", "rebooting", "":
		return ObservedPending, false
	case "stopped", "frozen":
		return ObservedStopped, false
	case "exited", "unknown", "offline":
		return ObservedError, true
	default:
		return ObservedPending, false
	}
}

// Status polls one instance.
func (v *VastAIProvider) Status(ctx context.Context, id string) (*InstanceStatus, error) {
	var resp struct {
		Instances vastInstance `json:"instances"`
	}
	if err := v.do(ctx, http.MethodGet, "/api/v0/instances/"+id+"/", nil, &resp); err != nil {
		if err == ErrNotFound {
			return &InstanceStatus{ProviderInstanceID: id, State: ObservedGone, Terminal: true}, nil
		}
		return nil, err
	}
	state, terminal := normalizeVastState(resp.Instances.ActualStatus)
	return &InstanceStatus{
		ProviderInstanceID: id,
		State:              state,
		Terminal:           terminal,
		Message:            resp.Instances.StatusMsg,
	}, nil
}

// ListOwned returns every instance on the account, keyed by label.
//
// Uses the paginated list endpoint rather than polling each instance: the
// per-instance rate limit makes N individual GETs impractical for a fleet.
func (v *VastAIProvider) ListOwned(ctx context.Context) (map[string]InstanceStatus, error) {
	var resp struct {
		Instances []vastInstance `json:"instances"`
	}
	if err := v.do(ctx, http.MethodGet, "/api/v0/instances/?owner=me", nil, &resp); err != nil {
		return nil, err
	}

	out := make(map[string]InstanceStatus, len(resp.Instances))
	for _, in := range resp.Instances {
		if in.Label == "" {
			continue // not ours, or created outside KrakenHashes
		}
		state, terminal := normalizeVastState(in.ActualStatus)
		out[in.Label] = InstanceStatus{
			ProviderInstanceID: fmt.Sprintf("%d", in.ID),
			State:              state,
			Terminal:           terminal,
			Message:            in.StatusMsg,
		}
	}
	return out, nil
}

// Destroy deletes an instance.
//
// Always DELETE, never stop: a stopped Vast.ai instance keeps billing storage
// and can land in an un-restartable scheduling limbo. Already-gone is success,
// as the Provider contract requires.
func (v *VastAIProvider) Destroy(ctx context.Context, id string) error {
	err := v.do(ctx, http.MethodDelete, "/api/v0/instances/"+id+"/", map[string]interface{}{}, nil)
	if err == ErrNotFound {
		return nil
	}
	return err
}

// CostSoFar sums this instance's charges.
//
// `day` bounds are required by the endpoint; omitting them is a 400. The
// per-instance figure is authoritative once it appears, but it lags, so a
// zero result means "keep using the wall-clock estimate", not "free".
func (v *VastAIProvider) CostSoFar(ctx context.Context, id string) (int64, bool, error) {
	now := time.Now()
	start := now.AddDate(0, 0, -31)
	path := fmt.Sprintf("/api/v0/charges?select_filters=%s&limit=100",
		fmt.Sprintf(`{"day":{"gte":%d,"lte":%d}}`, start.Unix(), now.Unix()))

	var resp struct {
		Results []struct {
			Source string  `json:"source"`
			Amount float64 `json:"amount"`
		} `json:"results"`
	}
	if err := v.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return 0, false, err
	}

	want := "instance-" + id
	var total float64
	var found bool
	for _, r := range resp.Results {
		if r.Source == want {
			total += r.Amount
			found = true
		}
	}
	if !found {
		return 0, false, nil
	}
	return int64(total * 100), true, nil
}
