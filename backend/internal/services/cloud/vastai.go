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

// vastDefaultBaseURL is the marketplace API. Overridable per provider instance
// rather than a package const, so the adapter can be pointed at an httptest
// server — see VastAIProvider.BaseURL.
const vastDefaultBaseURL = "https://console.vast.ai"

/*
 * VastSettings is the operator's Vast.ai placement and hardware policy.
 *
 * THE ZERO VALUE IS TODAY'S BEHAVIOUR, exactly: verified datacenter hosts only,
 * every country, every card, no reliability floor. That matters because these
 * settings were never read before — cfg.Settings was ignored entirely for
 * Vast.ai — so every existing config deserialises to the zero value and must
 * not change what it rents.
 */
type VastSettings struct {
	/*
	 * Countries allowlists Vast.ai geolocations. Empty means anywhere.
	 *
	 * Matched against Offer.Region, which until now was written and never read
	 * anywhere in the codebase. Vast reports a coarse string ("US, Texas" or a
	 * bare country code), so matching is a case-insensitive prefix/substring
	 * rather than an equality test — see offerInRegion.
	 *
	 * Two purposes at once: data residency, and widening or narrowing the pool.
	 */
	Countries []string `json:"countries"`
	// GPUModels allowlists cards by normalised model key. Empty means any.
	GPUModels []string `json:"gpu_models"`
	// DeniedGPUModels wins over GPUModels, matching applyOfferConstraints.
	DeniedGPUModels []string `json:"denied_gpu_models"`
	/*
	 * AllowUnverified and AllowResidential open the two tiers this provider has
	 * always excluded unconditionally.
	 *
	 * DEFAULT FALSE, deliberately, and the danger is worth stating where the
	 * flag lives: unticking these is the single largest availability increase
	 * available on Vast.ai, AND it places client hash material on machines that
	 * have not even been through Vast's own verification. The provider is
	 * already peer hardware whose host has root over the container; these
	 * flags remove the one filter that keeps it to hosts Vast has checked.
	 *
	 * The unverified tier is also where "stuck connecting" and "bad driver"
	 * reports concentrate, so the extra availability is partly illusory: a
	 * rental that never reaches useful work still costs commissioning.
	 */
	AllowUnverified  bool `json:"allow_unverified"`
	AllowResidential bool `json:"allow_residential"`
	/*
	 * MinReliability is Vast's own host reliability score, 0..1. Zero disables
	 * the floor.
	 *
	 * The score was already captured into Offer.Raw and then read by nothing.
	 * Trading a little availability for fewer dead rentals is the point: a host
	 * that drops the instance mid-chunk has still been paid for its
	 * commissioning.
	 */
	MinReliability float64 `json:"min_reliability"`
}

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
	/*
	 * BaseURL is a field rather than a package constant purely so this adapter
	 * can be tested.
	 *
	 * That sounds like a detail and is not: with the URL compiled in there was
	 * no way to exercise a single line of this file without a funded Vast.ai
	 * account, so none of it was ever exercised at all. This provider is marked
	 * beta precisely because it has never been paid for, and a test seam is the
	 * only way to shrink that gap without spending money.
	 */
	BaseURL string
	// Settings is the operator's placement and hardware policy. The zero value
	// reproduces the historical behaviour exactly: verified datacenter hosts
	// only, no country or model restriction, no reliability floor.
	Settings VastSettings

	// rateMu serialises calls: Vast.ai enforces a minimum interval between
	// requests per endpoint and returns 429 with NO Retry-After header, so the
	// backoff has to be ours.
	rateMu   sync.Mutex
	lastCall time.Time
	// MinInterval is the floor between any two API calls.
	MinInterval time.Duration
}

// NewVastAIProvider creates a Vast.ai provider.
func NewVastAIProvider(apiKey string, settings VastSettings) *VastAIProvider {
	return &VastAIProvider{
		APIKey:      apiKey,
		BaseURL:     vastDefaultBaseURL,
		Settings:    settings,
		Client:      &http.Client{Timeout: 60 * time.Second},
		MinInterval: 3 * time.Second,
	}
}

// baseURL tolerates a zero-value provider built by a test or an older caller.
func (v *VastAIProvider) baseURL() string {
	if v.BaseURL != "" {
		return v.BaseURL
	}
	return vastDefaultBaseURL
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

	req, err := http.NewRequestWithContext(ctx, method, v.baseURL()+path, reader)
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
	ID            int    `json:"id"`
	AskContractID int    `json:"ask_contract_id"`
	GPUName       string `json:"gpu_name"`
	NumGPUs       int    `json:"num_gpus"`
	// GPURAM is per-GPU memory in MEGABYTES. Not GB: a 24GB card reports
	// ~24564. Reading it as GB would make every VRAM comparison off by 1024x,
	// which does not fail loudly — it just makes every floor pass and every
	// ceiling drop everything.
	GPURAM       float64 `json:"gpu_ram"`
	DPHTotal     float64 `json:"dph_total"`
	StorageCost  float64 `json:"storage_cost"`
	InetDownCost float64 `json:"inet_down_cost"`
	DiskSpace    float64 `json:"disk_space"`
	Duration     float64 `json:"duration"`
	/*
	 * Vast.ai spells the host reliability score BOTH ways depending on where you
	 * look: `reliability2` is the documented search-filter key and appears in
	 * bundles responses, while `reliability` shows up in older payloads. Only
	 * `reliability` was ever decoded here, and since nothing read the value it
	 * would have gone unnoticed if it were always zero.
	 *
	 * Both are accepted, and reliability() prefers whichever is populated. With
	 * no live account to settle which one this endpoint actually sends, decoding
	 * one and guessing is how a reliability floor silently deletes every offer.
	 */
	Reliability     float64 `json:"reliability"`
	Reliability2    float64 `json:"reliability2"`
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
// `duration` is SECONDS here (the CLI multiplies by 86400) and `gpu_ram` is
// MEGABYTES. Getting these wrong silently returns the wrong machines.
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
	}
	/*
	 * Verified datacenter hosts only, UNLESS the operator has explicitly opened
	 * the tier. This provider puts client hash material on third-party machines,
	 * so the cheaper unverified tier — which is also where the "stuck
	 * connecting" and "bad driver" reports concentrate — is excluded by default.
	 *
	 * Note the asymmetry in how the two states are expressed: to RESTRICT we
	 * send {eq: true}, but to OPEN we omit the key entirely rather than sending
	 * {eq: false}. Sending false would invert the filter and return ONLY
	 * unverified hosts, quietly excluding every good one — the failure would
	 * look like a mysterious drop in quality rather than a broken query.
	 */
	if !v.Settings.AllowUnverified {
		query["verified"] = map[string]interface{}{"eq": true}
	}
	if !v.Settings.AllowResidential {
		query["datacenter"] = map[string]interface{}{"eq": true}
	}
	if v.Settings.MinReliability > 0 {
		// reliability2 is the documented filter key. Advisory like every other
		// server-side filter here — offerReliability re-checks client-side.
		query["reliability2"] = map[string]interface{}{"gte": v.Settings.MinReliability}
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
	/*
	 * VRAM push-down, in MEGABYTES, deliberately half a GB slack in each
	 * direction.
	 *
	 * Vast.ai reports USABLE VRAM, so a 24GB card is 24564MB, not 24576. An
	 * exact `gte: 24*1024` would therefore reject every 24GB card an operator
	 * asked for — the server filter would be STRICTER than the rounding
	 * vastVRAMGB does client-side, and the two disagreeing is worse than not
	 * pushing down at all. The slack mirrors round-to-nearest exactly, so this
	 * can only ever return a superset of what applyOfferConstraints keeps.
	 * That is the intended relationship: this narrows the response, the shared
	 * post-filter is the guarantee.
	 */
	if q.MinVRAMGBPerGPU > 0 || q.MaxVRAMGBPerGPU > 0 {
		gpuRAM := map[string]interface{}{}
		if q.MinVRAMGBPerGPU > 0 {
			gpuRAM["gte"] = q.MinVRAMGBPerGPU*1024 - 512
		}
		if q.MaxVRAMGBPerGPU > 0 {
			gpuRAM["lte"] = q.MaxVRAMGBPerGPU*1024 + 512
		}
		query["gpu_ram"] = gpuRAM
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
			VRAMGBPerGPU:        vastVRAMGB(o.GPURAM),
			// Vast.ai has no stock enum to normalise. Its rentable/rented/
			// verified filters already serve that role, so claiming a level
			// here would be inventing data — unknown is the honest answer and
			// passes any MinAvailability floor.
			Availability: AvailabilityUnknown,
			Raw: models.JSONMap{
				"reliability": o.reliability(),
				"cuda":        o.CudaMaxGood,
				"verified":    o.Verified,
				"inet_down":   o.InetDown,
			},
		})
	}
	// The server-side filters above are advisory — `rented` is proof of that —
	// so the query's real guarantee is this, applied to whatever came back.
	return applyOfferConstraints(offers, v.narrowQuery(q)), nil
}

/*
 * narrowQuery folds the operator's Vast.ai policy into the caller's query.
 *
 * The caller builds one provider-agnostic OfferQuery and cannot know a
 * provider's own settings, so the provider is where the two meet. Everything
 * here NARROWS except one thing, which is called out below.
 *
 * Both lists are intersected rather than replaced. The caller's constraints
 * come from the job and the client's cost ceiling; the operator's come from
 * policy, and neither is entitled to widen the other.
 */
func (v *VastAIProvider) narrowQuery(q OfferQuery) OfferQuery {
	q.AllowedRegions = intersectOrUnion(q.AllowedRegions, v.Settings.Countries)
	q.AllowedGPUModels = intersectOrUnion(q.AllowedGPUModels, v.Settings.GPUModels)
	// Deny lists are the one thing that accumulates: a deny is an emergency
	// lever and must never be weakened by anything, including an intersection.
	q.DeniedGPUModels = append(append([]string(nil), q.DeniedGPUModels...), v.Settings.DeniedGPUModels...)

	if v.Settings.MinReliability > q.MinReliability {
		q.MinReliability = v.Settings.MinReliability
	}

	/*
	 * THE ONE RELAXATION, and it is deliberate.
	 *
	 * The caller hardcodes VerifiedOnly (service.go's offer search) as a safe
	 * default from before this setting existed. AllowUnverified is a specific,
	 * acknowledged decision by the operator to accept unverified hosts on THIS
	 * provider — so leaving the caller's blanket default in place would make the
	 * setting silently do nothing: the server would return unverified offers and
	 * applyOfferConstraints would delete every one of them, with the operator
	 * seeing "no offers" and no indication why.
	 *
	 * Scoped to Vast.ai, because it is the only provider whose settings can
	 * express the consent. Nothing else is relaxed here.
	 */
	if v.Settings.AllowUnverified {
		q.VerifiedOnly = false
	}
	return q
}

/*
 * intersectOrUnion combines two allow lists where EMPTY MEANS EVERYTHING.
 *
 * That convention makes plain intersection wrong: intersecting anything with an
 * empty list would yield an empty list, i.e. "nothing allowed", which is the
 * exact inversion of what empty means. So an empty side yields the other side,
 * and only when both are populated do they actually intersect.
 *
 * If two populated lists share nothing, the result is a single sentinel that
 * matches no real value. Returning empty there would read as "no restriction"
 * and rent from anywhere — silently ignoring both parties' constraints at the
 * exact moment they disagree.
 */
func intersectOrUnion(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[strings.ToLower(strings.TrimSpace(s))] = true
	}
	var out []string
	for _, s := range a {
		if inB[strings.ToLower(strings.TrimSpace(s))] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"\x00none"}
	}
	return out
}

/*
 * vastVRAMGB converts Vast.ai's per-GPU megabytes into whole gigabytes.
 *
 * Rounds to NEAREST rather than truncating, because the API reports usable
 * VRAM: a 24GB card is 24564MB, and truncation would call it 23GB and fail a
 * 24GB floor the operator set precisely to get that card. Zero in stays zero
 * out — that is UNKNOWN, and the filter must never read it as "no VRAM".
 */
func vastVRAMGB(megabytes float64) int {
	if megabytes <= 0 {
		return 0
	}
	return (int(megabytes) + 512) / 1024
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

// reliability returns whichever spelling of the host reliability score the
// response actually carried. Zero means the endpoint reported neither, which
// offerReliability reads as unknown rather than as a failing host.
func (o vastOffer) reliability() float64 {
	if o.Reliability2 > 0 {
		return o.Reliability2
	}
	return o.Reliability
}
