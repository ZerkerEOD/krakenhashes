package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

const (
	runpodDefaultRESTBase = "https://rest.runpod.io/v1"
	runpodDefaultGraphQL  = "https://api.runpod.io/graphql"
)

/*
 * runpodLabelRe matches exactly the labels service.go produces:
 * "kh-" + uuid.String()[:18], i.e. kh-xxxxxxxx-xxxx-xxxx.
 *
 * ANCHORED, and that anchoring is the whole security property. RunPod has no
 * server-side ownership tag — a pod's NAME is all we get — so an unanchored
 * match would adopt any pod an operator happened to call "my-kh-test", and
 * adoption here means eligibility for automatic destruction.
 *
 * Weaker than AWS's tag:krakenhashes:managed filter, which the provider itself
 * enforces. A name is guessable and a tag is not, which is why a DEDICATED
 * RunPod account is a requirement here rather than advice.
 */
var runpodLabelRe = regexp.MustCompile(`^kh-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}$`)

/*
 * RunPodSettings is the non-secret provider configuration.
 *
 * NOTE WHAT IS ABSENT, because both absences are load-bearing:
 *
 *   - No instance_type_rates equivalent. AWS needs one because its Pricing API
 *     silently returns the wrong SKU without exact filters; RunPod returns live
 *     prices from gpuTypes.lowestPrice, so an operator-declared rate would be a
 *     second source of truth that can only ever be more wrong than the first —
 *     and reservations are denominated in whatever it says.
 *   - No volume size. Network volumes OUTLIVE the pod, so one would retain
 *     cracked plaintexts after termination. Hard-coded to 0 on create and
 *     deliberately not configurable.
 */
type RunPodSettings struct {
	/*
	 * DataCenterIDs is both a residency control and the fallback dimension, and
	 * it works in the OPPOSITE direction from AWS's zones — worth stating,
	 * because the instinct carried over from that screen is wrong here.
	 *
	 * EMPTY (the default) means "let RunPod place it": no dataCenterIds on
	 * create, one offer per GPU type, and RunPod's own scheduler is free to try
	 * every data centre. This is the WIDEST pool. On AWS, omitting the subnet
	 * still lands you in exactly one availability zone, so there pinning WIDENS
	 * the candidate set; here pinning NARROWS it.
	 *
	 * Set it when data residency matters, or when you would rather the launch
	 * retry loop walk real independent pools than re-hit one exhausted global
	 * placement.
	 */
	DataCenterIDs []string `json:"data_center_ids"`
	// GPUTypeIDs allowlists RunPod gpuType ids ("NVIDIA GeForce RTX 4090").
	// Empty means every type the tier offers, subject to the shared filters.
	GPUTypeIDs []string `json:"gpu_type_ids"`
	/*
	 * Interruptible requests spot. DEFAULT FALSE and it should stay false until
	 * the Offer contract can express preemption honestly: MaxDuration 0 means
	 * UNBOUNDED, which is the exact opposite of the truth for a spot pod, so an
	 * interruptible pod currently sails through the caller's "must survive
	 * twice the TTL" filter.
	 */
	Interruptible bool `json:"interruptible"`
	// ContainerDiskGB is a FLOOR; the job's file set may require more.
	ContainerDiskGB int `json:"container_disk_gb"`
	// ContainerDiskCentsPerGBMonth feeds Offer.StorageCentsPerHour exactly as
	// AWSSettings.EBSCentsPerGBMonth does. Left at zero the default below
	// applies; disk is never simply un-budgeted, because the reservation is the
	// only thing that makes a spend cap real.
	ContainerDiskCentsPerGBMonth float64 `json:"container_disk_cents_per_gb_month"`
	/*
	 * AllowInGuestSelfDestruct injects a RunPod API key into the pod so the
	 * in-guest watchdog can DELETE its own pod when the VPN dies.
	 *
	 * REFUSED ON COMMUNITY, unconditionally, whatever this says. RunPod issues
	 * no per-pod scoped credential — Vast.ai's CONTAINER_API_KEY has no
	 * equivalent — so the only key that works is ACCOUNT-scoped: it can create
	 * pods and delete every other pod on the account. On Community the host
	 * operator has root over the container and would simply read it out of the
	 * environment.
	 */
	AllowInGuestSelfDestruct bool `json:"allow_in_guest_self_destruct"`
}

// defaultRunPodDiskCentsPerGBMonth is RunPod's published running container-disk
// rate ($0.10/GB-month). Wrong-region defaults beat budgeting zero for storage.
const defaultRunPodDiskCentsPerGBMonth = 10.0

/*
 * RunPodCredentials is the encrypted secret blob.
 *
 * A BARE STRING is also accepted and read as {api_key: "..."}, so the common
 * case stays as simple as Vast.ai's single token.
 */
type RunPodCredentials struct {
	APIKey string `json:"api_key"`
	/*
	 * SelfDestructAPIKey is a SEPARATE, narrower key handed to the guest.
	 *
	 * Kept apart from APIKey so an operator can scope it down to pod read/write
	 * without weakening the key the backend provisions with. Optional; when
	 * absent and self-destruct is enabled, APIKey is used and the log says so,
	 * because that is a materially worse exposure.
	 */
	SelfDestructAPIKey string `json:"self_destruct_api_key,omitempty"`
}

// parseRunPodCredentials tolerates both the bare-string and object forms.
func parseRunPodCredentials(raw string) RunPodCredentials {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RunPodCredentials{}
	}
	if strings.HasPrefix(raw, "{") {
		var c RunPodCredentials
		if err := json.Unmarshal([]byte(raw), &c); err == nil && c.APIKey != "" {
			return c
		}
	}
	return RunPodCredentials{APIKey: raw}
}

/*
 * RunPodProvider rents GPU pods from RunPod.
 *
 * ONE ADAPTER, TWO KINDS. models.CloudProviderRunPod and RunPodCommunity differ
 * only in a tier bit, and every tier-dependent value is DERIVED from kind
 * rather than stored beside it: a separate Secure bool next to a Name field is
 * two facts that can disagree, and the direction they disagree in is peer
 * hardware appearing under the tier a client acknowledged as RunPod's own
 * datacentres.
 *
 * Four properties shape this adapter, none of which Vast.ai shares:
 *
 * 1. NO IDEMPOTENCY KEY on POST /pods, AND no server-enforced unique name.
 *    Vast at least gives one contract per ask; here a retried create yields two
 *    pods with the SAME label. See ListOwned for how the second is surfaced
 *    rather than swallowed.
 *
 * 2. desiredStatus is DESIRED, not observed. It is the only status the list
 *    endpoint returns, so "RUNNING" means RunPod intends this to run, not that
 *    it is running. Never gate readiness on it.
 *
 * 3. NO PROVIDER TTL and no per-pod scoped credential the guest could use to
 *    delete itself. On Community that leaves the backend reaper as the only
 *    thing that can stop a pod billing.
 *
 * 4. Pods are unprivileged containers, exactly like Vast.ai: userspace VPN
 *    only, which the existing entrypoint already handles unchanged.
 */
type RunPodProvider struct {
	APIKey string
	// SelfDestructAPIKey is injected into Secure pods when the operator opted
	// in. Never injected on Community — see RunPodSettings.
	SelfDestructAPIKey string

	// kind is the tier. Everything tier-dependent derives from it.
	kind     models.CloudProvider
	settings RunPodSettings

	/*
	 * Base URLs are fields, not constants, so the adapter is testable against
	 * an httptest server.
	 *
	 * This is the difference between an adapter with tests and one without:
	 * vastai.go had a package-level const and consequently no test file at all
	 * for its entire life. RunPod has no account to verify against here, so a
	 * test seam is not a nicety — it is the only verification this code will
	 * get before a community tester points real money at it.
	 */
	RESTBaseURL string
	GraphQLURL  string

	Client *http.Client

	// Same self-imposed floor as Vast.ai: RunPod publishes per-key rate limits
	// and the backoff has to be ours.
	rateMu      sync.Mutex
	lastCall    time.Time
	MinInterval time.Duration
}

// NewRunPodProvider builds an adapter for one of the two RunPod tiers.
func NewRunPodProvider(creds RunPodCredentials, kind models.CloudProvider, s RunPodSettings) (*RunPodProvider, error) {
	switch kind {
	case models.CloudProviderRunPod, models.CloudProviderRunPodCommunity:
	default:
		return nil, fmt.Errorf("runpod: %q is not a RunPod tier", kind)
	}
	if creds.APIKey == "" {
		return nil, fmt.Errorf("runpod: provider has no API key")
	}

	p := &RunPodProvider{
		APIKey:      creds.APIKey,
		kind:        kind,
		settings:    s,
		RESTBaseURL: runpodDefaultRESTBase,
		GraphQLURL:  runpodDefaultGraphQL,
		Client:      &http.Client{Timeout: 60 * time.Second},
		MinInterval: 500 * time.Millisecond,
	}

	/*
	 * The Community refusal is enforced HERE, at construction, not at the point
	 * of use. A check next to the injection site is one refactor away from
	 * being bypassed by a second injection site; refusing to even hold the key
	 * means there is nothing to leak however the rest of the file changes.
	 */
	if s.AllowInGuestSelfDestruct {
		switch {
		case kind == models.CloudProviderRunPodCommunity:
			debug.Warning("Cloud/RunPod: allow_in_guest_self_destruct is set on the COMMUNITY tier and is " +
				"being ignored. RunPod issues no per-pod scoped credential, so the only usable key is " +
				"account-scoped — and a Community host operator has root over the container and would read " +
				"it out of the environment. Teardown on this tier is the backend reaper alone.")
		case creds.SelfDestructAPIKey != "":
			p.SelfDestructAPIKey = creds.SelfDestructAPIKey
		default:
			debug.Warning("Cloud/RunPod: in-guest self-destruct is enabled with no separate " +
				"self_destruct_api_key, so the PROVISIONING key will be placed inside each pod. That key " +
				"can create and delete every pod on the account. Supply a narrower key scoped to pod " +
				"read/write instead.")
			p.SelfDestructAPIKey = creds.APIKey
		}
	}
	return p, nil
}

// Name identifies the tier. The two must never both render as "RunPod".
func (r *RunPodProvider) Name() models.CloudProvider { return r.kind }

// secure reports whether this is the SECURE tier. THE ONLY tier predicate.
func (r *RunPodProvider) secure() bool { return r.kind == models.CloudProviderRunPod }

func (r *RunPodProvider) cloudType() string {
	if r.secure() {
		return "SECURE"
	}
	return "COMMUNITY"
}

func (r *RunPodProvider) restBase() string {
	if r.RESTBaseURL != "" {
		return r.RESTBaseURL
	}
	return runpodDefaultRESTBase
}

func (r *RunPodProvider) graphQL() string {
	if r.GraphQLURL != "" {
		return r.GraphQLURL
	}
	return runpodDefaultGraphQL
}

// runpodAmbiguousError marks a failure that MAY have created a pod. Wrapped
// rather than sentinel-compared because the useful distinction is "did the
// request reach RunPod", and only the transport layer knows.
type runpodAmbiguousError struct{ err error }

func (e *runpodAmbiguousError) Error() string { return e.err.Error() }
func (e *runpodAmbiguousError) Unwrap() error { return e.err }

/*
 * do performs a rate-limited, authenticated REST call.
 *
 * Distinguishes three outcomes rather than two, because Launch needs the
 * difference: a clean 4xx means nothing was created, while a transport error or
 * a 5xx means a pod MAY be billing with nobody accounting for it. Conflating
 * them is how an orphan happens.
 */
func (r *RunPodProvider) do(ctx context.Context, method, path string, body, out interface{}) error {
	r.rateMu.Lock()
	if wait := r.MinInterval - time.Since(r.lastCall); wait > 0 {
		select {
		case <-ctx.Done():
			r.rateMu.Unlock()
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	r.lastCall = time.Now()
	r.rateMu.Unlock()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("runpod: marshal request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, r.restBase()+path, reader)
	if err != nil {
		return fmt.Errorf("runpod: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		// The request may or may not have reached RunPod. Ambiguous.
		return &runpodAmbiguousError{fmt.Errorf("runpod: %s %s: %w", method, path, err)}
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return &runpodAmbiguousError{fmt.Errorf("runpod: read response: %w", err)}
	}
	trimmed := strings.TrimSpace(string(payload))

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("runpod: rate limited: %s", trimmed)
	case resp.StatusCode >= 500:
		// Server-side failure: the pod may exist regardless of what it says.
		return &runpodAmbiguousError{
			fmt.Errorf("runpod: %s %s returned %d: %s", method, path, resp.StatusCode, trimmed)}
	case resp.StatusCode >= 300:
		return fmt.Errorf("runpod: %s %s returned %d: %s", method, path, resp.StatusCode, trimmed)
	}

	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("runpod: decode response: %w", err)
		}
	}
	return nil
}

// runpodPod is the subset of the Pod object this adapter reads.
type runpodPod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// DesiredStatus is RUNNING | EXITED | TERMINATED. See normalizeRunPodState:
	// it is a DESIRED state and must never be read as an observation.
	DesiredStatus string  `json:"desiredStatus"`
	CostPerHr     float64 `json:"costPerHr"`
	MachineID     string  `json:"machineId"`
	PublicIP      string  `json:"publicIp"`
	// LastStartedAt is the closest thing to a creation time the object has;
	// there is no createdAt field.
	LastStartedAt    string `json:"lastStartedAt"`
	LastStatusChange string `json:"lastStatusChange"`
	Machine          struct {
		DataCenterID string `json:"dataCenterId"`
		GPUTypeID    string `json:"gpuTypeId"`
	} `json:"machine"`
}

/*
 * normalizeRunPodState maps RunPod's desiredStatus onto our vocabulary.
 *
 * IS desiredStatus A TRAP? Yes, but a bounded one worth writing down. It
 * over-claims Running: a pod queued or pulling a 12GB image reports
 * desiredStatus RUNNING. That would be dangerous if anything gated readiness on
 * ObservedRunning — nothing does. Readiness is the agent registering, enforced
 * by ready_deadline_at. It under-claims nothing, which is the direction that
 * costs money.
 *
 * Where it does bite is diagnosis: a pod wedged pulling an image is
 * indistinguishable from a healthy one, and the ready deadline is what ends it.
 * Status therefore reports the raw value in Message so an operator sees
 * "RUNNING (desired)" rather than a bare "running" contradicting the console.
 */
func normalizeRunPodState(desired string) (InstanceObservedState, bool) {
	switch strings.ToUpper(strings.TrimSpace(desired)) {
	case "RUNNING":
		return ObservedRunning, false
	case "EXITED":
		/*
		 * Terminal DELIBERATELY, even though a stopped pod can be resumed. We
		 * never resume: a stopped RunPod pod keeps billing container disk at
		 * roughly double the running rate, so "recoverable" and "should be
		 * destroyed" are both true and only one of them saves money.
		 */
		return ObservedStopped, true
	case "TERMINATED":
		return ObservedGone, true
	case "":
		return ObservedPending, false
	default:
		return ObservedPending, false
	}
}

func runpodStatusOf(p runpodPod) InstanceStatus {
	state, terminal := normalizeRunPodState(p.DesiredStatus)
	msg := p.DesiredStatus + " (desired)"
	if p.LastStatusChange != "" {
		msg += "; " + p.LastStatusChange
	}
	return InstanceStatus{
		ProviderInstanceID: p.ID,
		State:              state,
		Terminal:           terminal,
		Message:            msg,
		Raw: models.JSONMap{
			"cost_per_hr":    p.CostPerHr,
			"machine_id":     p.MachineID,
			"data_center_id": p.Machine.DataCenterID,
		},
	}
}

// listPods returns every pod on the account.
func (r *RunPodProvider) listPods(ctx context.Context) ([]runpodPod, error) {
	var pods []runpodPod
	if err := r.do(ctx, http.MethodGet, "/pods", nil, &pods); err != nil {
		return nil, err
	}
	/*
	 * Pagination is UNDOCUMENTED, and this is the highest-consequence unknown
	 * in the adapter: ListOwned is the backstop that makes every "the reaper
	 * will adopt it later" argument sound, so a silently truncated list means
	 * orphans past page one bill indefinitely with nothing to notice them.
	 *
	 * A suspiciously round count is the only client-side hint available.
	 */
	if n := len(pods); n > 0 && (n%50 == 0 || n%100 == 0) {
		debug.Warning("Cloud/RunPod: GET /pods returned exactly %d pods. RunPod documents no pagination, "+
			"but if it silently pages then any orphan beyond the first page is INVISIBLE to the reaper "+
			"and will bill until someone notices by hand. Verify against the account.", n)
	}
	return pods, nil
}

/*
 * ListOwned returns every pod we own, keyed by label. The orphan-detection
 * primitive: anything here without a matching database row is billing with
 * nobody accounting for it.
 *
 * Lists everything and filters client-side. A ?name= filter cannot do this job
 * — labels are unique per instance, so the useful query is precisely "give me
 * all of them" and then match. Pods that fail the anchored regex are skipped,
 * exactly as an unlabelled Vast.ai instance or an untagged EC2 instance is.
 */
func (r *RunPodProvider) ListOwned(ctx context.Context) (map[string]InstanceStatus, error) {
	pods, err := r.listPods(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]InstanceStatus, len(pods))
	// Keyed by label so a clash can be resolved against the POD, not against an
	// already-flattened InstanceStatus. Reaching into Raw for a field the
	// mapper may not have written is how this panicked the first time it ran.
	seen := make(map[string]runpodPod, len(pods))

	for _, p := range pods {
		if !runpodLabelRe.MatchString(p.Name) {
			continue
		}
		prev, clash := seen[p.Name]
		if !clash {
			seen[p.Name] = p
			out[p.Name] = runpodStatusOf(p)
			continue
		}

		/*
		 * TWO PODS, ONE LABEL — the signature of a double launch. The create
		 * call has no idempotency key, so a retry that got through produces
		 * exactly this, and both pods are billing.
		 *
		 * A plain map assignment would DROP one of them, and the dropped pod
		 * would then never appear in any inventory again: invisible, unowned
		 * and billing forever. Instead the loser is re-keyed under a synthetic
		 * label that can never match a database row, so the existing orphan
		 * machinery finds it and destroys it after OrphanGrace — no interface
		 * change, and no destructive action taken from inside a read path.
		 *
		 * The synthetic key contains the pod id so it is STABLE across sweeps,
		 * which the orphan grace timer requires; a key that changed each sweep
		 * would reset the clock and the grace period would never elapse.
		 */
		/*
		 * OLDEST WINS the canonical label: it is the more likely to be the one
		 * the database row already points at, so keeping it avoids re-pointing
		 * a live row at a pod the backend has never heard of.
		 *
		 * Timestamps are compared as strings, which is correct for RFC3339 and
		 * degrades safely when either is empty — an empty string sorts first,
		 * so a pod with no timestamp is treated as older. Nothing depends on
		 * getting the tie exactly right: both pods end up in the map either
		 * way, which is the whole point.
		 */
		keep, drop := prev, p
		if p.LastStartedAt != "" && (prev.LastStartedAt == "" || p.LastStartedAt < prev.LastStartedAt) {
			keep, drop = p, prev
		}
		seen[p.Name] = keep
		out[p.Name] = runpodStatusOf(keep)
		out[fmt.Sprintf("%s#dup:%s", p.Name, drop.ID)] = runpodStatusOf(drop)
		debug.Error("Cloud/RunPod: label %s maps to TWO pods (%s and %s). This is a double launch: "+
			"POST /pods has no idempotency key. The second is billing unaccounted for and will be "+
			"reaped as an orphan.", p.Name, keep.ID, drop.ID)
	}
	return out, nil
}

// findByLabel returns the pod carrying a label, or nil.
//
// Uses the server-side name filter as an OPTIMISATION ONLY and always re-checks
// the name in Go: the filter's semantics (exact, prefix or substring) are
// undocumented, and an unverified substring match would adopt the wrong pod.
func (r *RunPodProvider) findByLabel(ctx context.Context, label string) (*runpodPod, error) {
	var pods []runpodPod
	err := r.do(ctx, http.MethodGet, "/pods?name="+url.QueryEscape(label), nil, &pods)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		// Fall back to the unfiltered scan: a broken filter must not be able to
		// hide an existing pod and cause a duplicate paid launch.
		pods, err = r.listPods(ctx)
		if err != nil {
			return nil, err
		}
	}
	for i := range pods {
		if pods[i].Name == label {
			return &pods[i], nil
		}
	}
	return nil, nil
}

/*
 * Launch creates one pod.
 *
 * THE MISSING IDEMPOTENCY KEY IS THE HARD PART. POST /pods has none and RunPod
 * does not enforce unique names, so a lost response can leave a pod billing
 * whose id we never saw. Three defences, in order:
 *
 *  1. Adopt before creating. The label is the idempotency key, as on Vast.ai.
 *  2. On an AMBIGUOUS failure — transport error or 5xx, where the request may
 *     have been accepted — reconcile by name before giving up. This is the only
 *     window in which the pod id can still be returned to the caller and the
 *     database row kept authoritative.
 *  3. Anything unclassified falls through as a plain error, leaving the row in
 *     `requested` for the reaper to adopt by label on its next sweep.
 *
 * The residual window is bounded by one reaper interval in the good case and
 * OrphanGrace plus a sweep in the bad one. What is NOT covered is RunPod
 * creating two pods for one request; ListOwned makes that visible instead.
 */
func (r *RunPodProvider) Launch(ctx context.Context, req LaunchRequest) (*LaunchResult, error) {
	if existing, err := r.findByLabel(ctx, req.Label); err == nil && existing != nil {
		debug.Warning("Cloud/RunPod: label %s already exists (pod %s); adopting it instead of creating "+
			"a duplicate paid pod", req.Label, existing.ID)
		return runpodAdopt(existing), nil
	}

	diskGB := req.DiskGB
	if r.settings.ContainerDiskGB > diskGB {
		diskGB = r.settings.ContainerDiskGB
	}

	env := make(map[string]string, len(req.Env)+1)
	for k, v := range req.Env {
		env[k] = v
	}
	// Secure tier only, and only when the operator opted in — see the
	// constructor, which refuses to even hold this key on Community.
	if r.SelfDestructAPIKey != "" {
		env["KH_RUNPOD_API_KEY"] = r.SelfDestructAPIKey
	}

	body := map[string]interface{}{
		"name":              req.Label,
		"cloudType":         r.cloudType(),
		"gpuTypeIds":        []string{req.Offer.InstanceType},
		"gpuCount":          maxInt(req.Offer.GPUCount, 1),
		"imageName":         req.Image,
		"env":               env,
		"containerDiskInGb": diskGB,
		// NEVER a network volume: it outlives the pod and would retain cracked
		// plaintexts after termination.
		"volumeInGb":    0,
		"interruptible": r.settings.Interruptible,
	}
	if req.Offer.PlacementRef != "" {
		body["dataCenterIds"] = []string{req.Offer.PlacementRef}
	}

	var pod runpodPod
	err := r.do(ctx, http.MethodPost, "/pods", body, &pod)
	if err == nil {
		if pod.ID == "" {
			return nil, fmt.Errorf("runpod: create returned no pod id")
		}
		r.warnOnPriceDrift(req.Offer, pod)
		now := time.Now()
		return &LaunchResult{
			ProviderInstanceID: pod.ID,
			LaunchedAt:         now,
			// Billing starts at CREATE, not at ready: RunPod's minimum billing
			// bucket is one hour, so anchoring later systematically
			// under-reports spend.
			BilledFrom: now,
			Raw: models.JSONMap{
				"pod_id": pod.ID, "label": req.Label,
				"cost_per_hr": pod.CostPerHr, "cloud_type": r.cloudType(),
			},
		}, nil
	}

	var ambiguous *runpodAmbiguousError
	if errors.As(err, &ambiguous) {
		if pod := r.reconcileByName(ctx, req.Label); pod != nil {
			debug.Warning("Cloud/RunPod: the create response was lost but pod %s exists under label %s; "+
				"adopting it rather than leaving it to bill unaccounted for", pod.ID, req.Label)
			return runpodAdopt(pod), nil
		}
		// Deliberately NOT ErrOfferUnavailable: see below.
		return nil, err
	}

	/*
	 * Only a genuine capacity refusal becomes ErrOfferUnavailable.
	 *
	 * attemptLaunch treats that sentinel as "no pod exists, nothing is billing"
	 * and RELEASES THE RESERVATION. Classifying an ambiguous timeout as one
	 * would free the budget for a pod that is quietly running, so the guard
	 * above returns the raw error instead and lets the reaper adopt by label.
	 */
	if isRunPodCapacityRefusal(err) {
		return nil, fmt.Errorf("%w: %w", ErrOfferUnavailable, err)
	}
	return nil, err
}

func runpodAdopt(p *runpodPod) *LaunchResult {
	now := time.Now()
	return &LaunchResult{
		ProviderInstanceID: p.ID,
		LaunchedAt:         now,
		BilledFrom:         now,
		Raw:                models.JSONMap{"adopted": true, "pod_id": p.ID, "cost_per_hr": p.CostPerHr},
	}
}

// reconcileByName polls briefly for a pod that may have been created by a
// request whose response was lost. Short and bounded: the reaper is the real
// backstop, this only shortens the window in which the row is not authoritative.
func (r *RunPodProvider) reconcileByName(ctx context.Context, label string) *runpodPod {
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Duration(attempt) * 5 * time.Second):
		}
		if pod, err := r.findByLabel(ctx, label); err == nil && pod != nil {
			return pod
		}
	}
	return nil
}

/*
 * warnOnPriceDrift compares what we reserved against what RunPod actually
 * charges, and is the only continuous check on the whole pricing path.
 *
 * It matters most for the one thing that could not be verified without an
 * account: whether lowestPrice is per-GPU or per-pod. Being wrong on a 4-GPU
 * pod under-reserves a client's cap by 4x, and a cap that can be exceeded is
 * not a cap. Offers are emitted single-GPU for exactly this reason, and this
 * log is what makes it safe to widen that later.
 */
func (r *RunPodProvider) warnOnPriceDrift(offer Offer, pod runpodPod) {
	if pod.CostPerHr <= 0 || offer.HourlyRateCents <= 0 {
		return
	}
	actual := centsFromDollarsCeil(pod.CostPerHr)
	diff := actual - offer.HourlyRateCents
	if diff < 0 {
		diff = -diff
	}
	if diff*10 > offer.HourlyRateCents {
		debug.Error("Cloud/RunPod: pod %s costs %dc/hr but the reservation was made at %dc/hr (>10%% drift). "+
			"The budget reserved the WRONG amount for this rental. If the ratio looks like the GPU count, "+
			"lowestPrice is per-GPU rather than per-pod and the offer builder needs correcting.",
			pod.ID, actual, offer.HourlyRateCents)
	}
}

/*
 * isRunPodCapacityRefusal decides whether "we could not get you a GPU" is what
 * RunPod actually said.
 *
 * Deliberately CONSERVATIVE: matching too broadly turns a real error into
 * ErrOfferUnavailable, which releases the reservation for a pod that may exist.
 * An unmatched error simply moves on to the next candidate more slowly.
 */
func isRunPodCapacityRefusal(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"no instances available",
		"no longer any instances available",
		"out of stock",
		"insufficient capacity",
		"unavailable",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

/*
 * Status polls one pod.
 *
 * Never maps a non-404 error to ObservedGone. ObservedGone is Terminal, and a
 * terminal status triggers destroy-and-finalize — so a transient 500 read that
 * way would finalize the row for a pod that is still running and still billing.
 */
func (r *RunPodProvider) Status(ctx context.Context, id string) (*InstanceStatus, error) {
	var pod runpodPod
	if err := r.do(ctx, http.MethodGet, "/pods/"+url.PathEscape(id), nil, &pod); err != nil {
		if errors.Is(err, ErrNotFound) {
			return &InstanceStatus{ProviderInstanceID: id, State: ObservedGone, Terminal: true}, nil
		}
		return nil, err
	}
	st := runpodStatusOf(pod)
	if st.ProviderInstanceID == "" {
		st.ProviderInstanceID = id
	}
	return &st, nil
}

/*
 * Destroy terminates a pod. Idempotent, and defensively so.
 *
 * RunPod's response to deleting a nonexistent pod could not be verified without
 * an account — 404, 400-with-a-message and 200 are all plausible — so all three
 * are treated as success. Guessing wrong in the other direction would leave the
 * reaper retrying forever against a pod that no longer exists.
 */
func (r *RunPodProvider) Destroy(ctx context.Context, id string) error {
	if id == "" {
		/*
		 * NOT "already gone". A blank id means the caller lost track of what to
		 * delete, and returning nil would finalize the database row for a pod
		 * that is still billing. Erroring keeps the row alive so the reaper
		 * reconciles it by label instead.
		 */
		return fmt.Errorf("runpod: destroy called with no pod id")
	}

	err := r.do(ctx, http.MethodDelete, "/pods/"+url.PathEscape(id), nil, nil)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound), isRunPodNotFoundBody(err):
		/*
		 * Already gone is success — the Provider contract requires convergence,
		 * and RunPod cannot bill for what does not exist.
		 *
		 * Logged rather than swallowed, because a 404 here is ALSO what a wrong
		 * base URL or a renamed route looks like, and in that case "already
		 * gone" is a lie that finalizes a live billing pod. The backstop is the
		 * reaper: once the row is finalized the label leaves the known set, the
		 * pod reappears as an orphan on the next inventory and is destroyed
		 * again. That convergence is what makes 404-as-success safe at all.
		 */
		debug.Warning("Cloud/RunPod: pod %s was already gone on delete", id)
		return nil
	}
	return err
}

// isRunPodNotFoundBody catches a "pod not found" reported with a 4xx other than
// 404, which the API may do. Matched on the body text this adapter's do() folds
// into the error string.
func isRunPodNotFoundBody(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
}

/*
 * CostSoFar always returns "no authoritative figure".
 *
 * costPerHr x elapsed is available and is NOT used, on purpose. That product is
 * precisely the caller's own wall-clock estimate, so returning it as
 * authoritative would launder an estimate into a fact and stop the caller
 * correcting it later. It is also wrong in the direction that matters under
 * RunPod's one-hour minimum billing bucket: at minute five the real charge is a
 * full hour, while the "authoritative" figure would claim 8% of one.
 *
 * Matches the AWS adapter, which declines for the same reason.
 */
func (r *RunPodProvider) CostSoFar(ctx context.Context, id string) (int64, bool, error) {
	return 0, false, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
