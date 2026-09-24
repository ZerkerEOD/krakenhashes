package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
 * The GraphQL side of the RunPod adapter: what hardware exists, where, at what
 * price, and how much of it is free.
 *
 * RunPod's availability data is the BEST of the three providers here, which is
 * worth saying because the screens look alike. AWS offers a catalogue fact plus
 * an advisory score measured contradicting reality. Vast.ai returns a count of
 * real machines. RunPod returns rentedCount and totalCount — actual inventory,
 * with the denominator included.
 */

// runpodGPUType is the subset of GpuType this adapter reads.
type runpodGPUType struct {
	ID             string  `json:"id"`
	DisplayName    string  `json:"displayName"`
	MemoryInGb     int     `json:"memoryInGb"`
	MaxGpuCount    int     `json:"maxGpuCount"`
	SecureCloud    bool    `json:"secureCloud"`
	CommunityCloud bool    `json:"communityCloud"`
	SecurePrice    float64 `json:"securePrice"`
	CommunityPrice float64 `json:"communityPrice"`

	LowestPrice *runpodLowestPrice `json:"lowestPrice"`
	// ByDC holds the aliased per-data-centre lowestPrice results, decoded
	// separately because GraphQL aliases are dynamic field names.
	ByDC map[string]*runpodLowestPrice `json:"-"`
}

type runpodLowestPrice struct {
	MinimumBidPrice      float64 `json:"minimumBidPrice"`
	UninterruptablePrice float64 `json:"uninterruptablePrice"`
	StockStatus          string  `json:"stockStatus"`
	RentedCount          int     `json:"rentedCount"`
	TotalCount           int     `json:"totalCount"`
}

// price picks the rate that matches how the pod will actually be bought.
func (l *runpodLowestPrice) price(interruptible bool) float64 {
	if l == nil {
		return 0
	}
	if interruptible {
		return l.MinimumBidPrice
	}
	return l.UninterruptablePrice
}

/*
 * doGraphQL posts a query to RunPod's GraphQL endpoint.
 *
 * Shares the REST client's rate limiter deliberately: RunPod's limits are
 * per-key, not per-endpoint, so two independently-throttled paths would breach
 * a limit neither of them thinks it is near.
 */
func (r *RunPodProvider) doGraphQL(ctx context.Context, query string, out interface{}) error {
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

	payload, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return fmt.Errorf("runpod: marshal graphql query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.graphQL(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("runpod: build graphql request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.Client.Do(req)
	if err != nil {
		return fmt.Errorf("runpod: graphql: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("runpod: read graphql response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("runpod: graphql returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	/*
	 * GraphQL reports errors with HTTP 200, so a status check is not enough.
	 * Treating a 200-with-errors as success would surface an auth failure as
	 * "no GPU types exist", which reads as an empty market rather than a
	 * broken credential.
	 */
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("runpod: decode graphql envelope: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("runpod: graphql error: %s", envelope.Errors[0].Message)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("runpod: decode graphql data: %w", err)
		}
	}
	return nil
}

// dcAlias makes a data-centre id safe as a GraphQL field alias (EU-RO-1 has a
// hyphen, which GraphQL names cannot contain).
func dcAlias(dc string) string {
	return "dc_" + strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(dc)
}

/*
 * gpuTypesQuery builds ONE document that asks for every data centre at once,
 * using field aliases.
 *
 * One round trip regardless of how many data centres are configured. The
 * alternative — a call per data centre — multiplies against a per-key rate
 * limit whose exact value is unknown, on a provider where a 429 has no
 * Retry-After to obey.
 */
func (r *RunPodProvider) gpuTypesQuery(dataCenters []string) string {
	fields := "uninterruptablePrice minimumBidPrice stockStatus rentedCount totalCount"
	secure := "true"
	if !r.secure() {
		secure = "false"
	}

	var b strings.Builder
	b.WriteString("query { gpuTypes { id displayName memoryInGb maxGpuCount secureCloud communityCloud ")
	b.WriteString(fmt.Sprintf("lowestPrice(input:{gpuCount:1, secureCloud:%s}) { %s } ", secure, fields))
	for _, dc := range dataCenters {
		b.WriteString(fmt.Sprintf("%s: lowestPrice(input:{gpuCount:1, secureCloud:%s, dataCenterId:%q}) { %s } ",
			dcAlias(dc), secure, dc, fields))
	}
	b.WriteString("} }")
	return b.String()
}

// fetchGPUTypes runs the query and re-attaches the aliased per-DC results.
func (r *RunPodProvider) fetchGPUTypes(ctx context.Context, dataCenters []string) ([]runpodGPUType, error) {
	// Decoded twice: once into the typed struct, once as raw maps to recover
	// the dynamic alias fields the struct cannot name.
	var raw struct {
		GPUTypes []map[string]json.RawMessage `json:"gpuTypes"`
	}
	if err := r.doGraphQL(ctx, r.gpuTypesQuery(dataCenters), &raw); err != nil {
		return nil, err
	}

	out := make([]runpodGPUType, 0, len(raw.GPUTypes))
	for _, entry := range raw.GPUTypes {
		merged, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		var gt runpodGPUType
		if err := json.Unmarshal(merged, &gt); err != nil {
			continue
		}
		gt.ByDC = map[string]*runpodLowestPrice{}
		for _, dc := range dataCenters {
			if blob, ok := entry[dcAlias(dc)]; ok {
				var lp runpodLowestPrice
				if json.Unmarshal(blob, &lp) == nil {
					gt.ByDC[dc] = &lp
				}
			}
		}
		out = append(out, gt)
	}
	return out, nil
}

/*
 * runpodAvailability maps RunPod's stock signals onto the shared enum.
 *
 * Note the asymmetry, which is the whole point: totalCount <= 0 is UNKNOWN (the
 * provider told us nothing) while totalCount > 0 with none free is a real,
 * reportable NONE. Collapsing those two is the Unknown-vs-None trap the Offer
 * contract warns about, and it fails in the expensive direction — Unknown
 * passes every floor, None is a definite "do not bother".
 */
func runpodAvailability(l *runpodLowestPrice) OfferAvailability {
	if l == nil {
		return AvailabilityUnknown
	}
	switch strings.ToLower(strings.TrimSpace(l.StockStatus)) {
	case "high":
		return AvailabilityHigh
	case "medium":
		return AvailabilityMedium
	case "low":
		return AvailabilityLow
	case "none":
		return AvailabilityNone
	case "":
		// No stockStatus; fall through to the counts.
	default:
		// An unrecognised value degrades to the counts rather than to a guess,
		// and says so once so the mapping can be corrected rather than
		// silently rotting.
		debug.Debug("Cloud/RunPod: unrecognised stockStatus %q; falling back to rented/total counts",
			l.StockStatus)
	}

	if l.TotalCount <= 0 {
		return AvailabilityUnknown
	}
	switch free := l.TotalCount - l.RentedCount; {
	case free <= 0:
		return AvailabilityNone
	case free <= 2:
		return AvailabilityLow
	case free <= 10:
		return AvailabilityMedium
	default:
		return AvailabilityHigh
	}
}

// centsFromDollarsCeil converts a dollar rate to cents, ROUNDING UP.
//
// Vast.ai truncates, and at RunPod's granularity ($0.34/hr, $0.69/hr)
// truncation systematically under-states the rate the budget reserves against.
// A spend cap is only as hard as the arithmetic behind it.
func centsFromDollarsCeil(usd float64) int {
	if usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0
	}
	return int(math.Ceil(usd * 100))
}

/*
 * SearchOffers turns RunPod's GPU catalogue into offers.
 *
 * PLACEMENT WORKS BACKWARDS FROM AWS, and getting this wrong would quietly
 * shrink the pool rather than grow it. With no data_center_ids configured,
 * RunPod's own scheduler may place a pod in ANY data centre, so one unpinned
 * offer per GPU type is the widest possible search. Pinning narrows it. On AWS
 * the reverse holds — omitting the subnet still lands in exactly one zone — so
 * the instinct carried over from that adapter is inverted here.
 *
 * GPUCount is 1 on every offer in this version. It is unverified whether
 * lowestPrice is quoted per-GPU or per-pod, and being wrong on a 4-GPU pod
 * under-reserves a client's cap by 4x. maxGpuCount is recorded in Raw, and
 * warnOnPriceDrift compares the launched pod's real costPerHr against the offer
 * on every launch, so the question answers itself in the logs before anyone
 * relies on it.
 */
func (r *RunPodProvider) SearchOffers(ctx context.Context, q OfferQuery) ([]Offer, error) {
	types, err := r.fetchGPUTypes(ctx, r.settings.DataCenterIDs)
	if err != nil {
		return nil, err
	}

	allowedByOperator := lowerSet(r.settings.GPUTypeIDs)
	diskGB := q.MinDiskGB
	if r.settings.ContainerDiskGB > diskGB {
		diskGB = r.settings.ContainerDiskGB
	}
	storageCents := r.diskCentsPerHour(diskGB)

	var offers []Offer
	var droppedNoStock int

	for _, gt := range types {
		// A tier that does not carry this card can never launch it.
		if r.secure() && !gt.SecureCloud {
			continue
		}
		if !r.secure() && !gt.CommunityCloud {
			continue
		}
		if len(allowedByOperator) > 0 && !allowedByOperator[strings.ToLower(gt.ID)] {
			continue
		}

		// Unpinned when no data centre is configured — the widest search.
		placements := map[string]*runpodLowestPrice{"": gt.LowestPrice}
		if len(r.settings.DataCenterIDs) > 0 {
			placements = map[string]*runpodLowestPrice{}
			for _, dc := range r.settings.DataCenterIDs {
				placements[dc] = gt.ByDC[dc]
			}
		}

		for dc, lp := range placements {
			avail := runpodAvailability(lp)
			/*
			 * AvailabilityNone is DATA, not absence of data, so dropping it
			 * here does not violate the unknown-passes rule. The alternative is
			 * handing the retry loop a candidate guaranteed to fail — and each
			 * failure costs a budget plan, a database row, a claim voucher and
			 * a reservation before the create is even attempted.
			 */
			if avail == AvailabilityNone {
				droppedNoStock++
				continue
			}

			cents := centsFromDollarsCeil(lp.price(r.settings.Interruptible))
			if cents <= 0 {
				// No price means nothing to reserve against, and a reservation
				// is what makes the client's cap real.
				continue
			}

			id := gt.ID
			if dc != "" {
				id = gt.ID + "@" + dc
			}
			model := gt.DisplayName
			if model == "" {
				model = gt.ID
			}

			offers = append(offers, Offer{
				ID: id,
				// The RAW gpuType id, because that is what gpuTypeIds takes on
				// create. DisplayName would not round-trip.
				InstanceType:        gt.ID,
				GPUModel:            model,
				GPUCount:            1,
				VRAMGBPerGPU:        gt.MemoryInGb,
				HourlyRateCents:     cents,
				StorageCentsPerHour: storageCents,
				// RunPod does not charge egress.
				BandwidthCentsPerGB: 0,
				Region:              dc,
				Zone:                dc,
				ZoneID:              dc,
				PlacementRef:        dc,
				Availability:        avail,
				Raw:                 r.offerRaw(gt, lp, dc),
			})
		}
	}

	if droppedNoStock > 0 {
		debug.Info("Cloud/RunPod: %d (gpu type, data centre) pairs reported zero stock and were dropped "+
			"before ranking; each would have cost a reservation and a database row to discover at launch",
			droppedNoStock)
	}

	offers = applyOfferConstraints(offers, r.narrowQuery(q))
	if len(offers) == 0 {
		return nil, fmt.Errorf("runpod: no %s GPU type satisfies the request", r.cloudType())
	}
	sort.SliceStable(offers, func(i, j int) bool {
		return offers[i].HourlyRateCents < offers[j].HourlyRateCents
	})
	return offers, nil
}

/*
 * offerRaw carries the provider's own view forward, and decides the one field
 * that can silently disable a whole tier.
 *
 * Raw["verified"] is TRUE on Secure and ABSENT on Community — never false. The
 * caller's offer search hardcodes VerifiedOnly, and offerVerified drops an
 * offer only when the flag is present AND false. Setting it false on Community
 * "for symmetry" would therefore delete every Community offer at the filter
 * stage, and runpod_community would be a provider that can be configured,
 * enabled and allowlisted while never once producing a candidate. Absent means
 * unknown, and unknown passes.
 */
func (r *RunPodProvider) offerRaw(gt runpodGPUType, lp *runpodLowestPrice, dc string) models.JSONMap {
	raw := models.JSONMap{
		"gpu_type_id":    gt.ID,
		"data_center_id": dc,
		"max_gpu_count":  gt.MaxGpuCount,
		"cloud_type":     r.cloudType(),
	}
	if lp != nil {
		raw["rented_count"] = lp.RentedCount
		raw["total_count"] = lp.TotalCount
		raw["stock_status"] = lp.StockStatus
		raw["uninterruptable_price"] = lp.UninterruptablePrice
		raw["minimum_bid_price"] = lp.MinimumBidPrice
	}
	if r.secure() {
		raw["verified"] = true
	}
	return raw
}

// narrowQuery folds the operator's RunPod policy into the caller's query, on the
// same terms as the Vast.ai adapter: allow lists intersect, deny lists grow.
func (r *RunPodProvider) narrowQuery(q OfferQuery) OfferQuery {
	if len(r.settings.DataCenterIDs) > 0 {
		q.AllowedRegions = intersectOrUnion(q.AllowedRegions, r.settings.DataCenterIDs)
	}
	return q
}

// diskCentsPerHour budgets container disk, rounding UP for the same reason the
// AWS EBS equivalent does: under-reserving lets a client exceed a cap they were
// told was hard.
func (r *RunPodProvider) diskCentsPerHour(diskGB int) int {
	if diskGB <= 0 {
		return 0
	}
	rate := r.settings.ContainerDiskCentsPerGBMonth
	if rate <= 0 {
		rate = defaultRunPodDiskCentsPerGBMonth
	}
	return int(math.Ceil(float64(diskGB) * rate / hoursPerMonth))
}

/*
 * Preflight verifies the key and reports what it can, in three layers.
 *
 * RunPod has no DryRun, so the write-capability probe deliberately sends a
 * request that is invalid at the SCHEMA level. A read-only API key is the most
 * valuable thing this check can find: without it, a restricted key surfaces
 * much later as a mysterious provisioning failure.
 */
func (r *RunPodProvider) Preflight(ctx context.Context) (*PreflightReport, error) {
	rep := &PreflightReport{}

	// 1. Read scope.
	var pods []runpodPod
	if err := r.do(ctx, http.MethodGet, "/pods", nil, &pods); err != nil {
		if strings.Contains(err.Error(), "401") {
			rep.Errors = append(rep.Errors, fmt.Sprintf("the API key was rejected: %v", err))
			return rep, nil
		}
		rep.MissingPermissions = append(rep.MissingPermissions,
			fmt.Sprintf("pods:read (GET /pods failed: %v)", err))
	}

	// 2. Identity and funding. The architecture notes claim RunPod has no
	// balance endpoint; GraphQL `myself` appears to contradict that, so it is
	// tried and its absence is reported rather than assumed either way.
	var me struct {
		Myself struct {
			ID            string  `json:"id"`
			Email         string  `json:"email"`
			ClientBalance float64 `json:"clientBalance"`
		} `json:"myself"`
	}
	if err := r.doGraphQL(ctx, "query { myself { id email clientBalance } }", &me); err != nil {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"could not read account balance (%v). Funding cannot be verified in advance on this provider; "+
				"a 402 at pod-create time would be the first sign of an empty account", err))
	} else {
		rep.Identity = me.Myself.Email
		rep.QuotaSource = "RunPod credit balance"
		rep.QuotaLimit = me.Myself.ClientBalance
		if me.Myself.ClientBalance <= 0 {
			rep.Errors = append(rep.Errors,
				"the RunPod account has no credit; every pod create will fail with a payment error")
		} else if me.Myself.ClientBalance < 5 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf(
				"RunPod credit is $%.2f, which is not much runway for a GPU rental", me.Myself.ClientBalance))
		}
	}

	// 3. Write capability, via a deliberately schema-invalid create.
	r.probeWriteScope(ctx, rep)

	rep.Warnings = append(rep.Warnings,
		"RunPod exposes no provider-side TTL, so nothing on their side will ever stop a pod. Teardown "+
			"rests on the in-guest deadline and the backend reaper.")
	if !r.secure() {
		rep.Warnings = append(rep.Warnings,
			"COMMUNITY TIER: pods run on peer-operated machines whose owners have root over the container, "+
				"and RunPod's SOC 2 / ISO 27001 / PCI DSS attestations do not cover this tier. Teardown here "+
				"is REAPER-ONLY: no per-pod scoped credential exists, so nothing inside the pod can stop it "+
				"billing if the backend is unavailable.")
	}

	rep.OK = len(rep.Errors) == 0 && len(rep.MissingPermissions) == 0 && len(rep.Inconclusive) == 0
	return rep, nil
}

func (r *RunPodProvider) probeWriteScope(ctx context.Context, rep *PreflightReport) {
	// containerDiskInGb: -1 is rejected by the schema, so a 400 proves the key
	// authenticated and carries write scope without creating anything.
	body := map[string]interface{}{
		"name": "kh-preflight", "cloudType": r.cloudType(),
		"imageName": "invalid", "containerDiskInGb": -1,
	}
	var created runpodPod
	err := r.do(ctx, http.MethodPost, "/pods", body, &created)

	switch {
	case err == nil:
		/*
		 * Should be impossible. If RunPod accepted a pod with a negative disk
		 * size, something is billing right now because of a health check.
		 */
		rep.Errors = append(rep.Errors, fmt.Sprintf(
			"the preflight write probe was ACCEPTED and created pod %s. It is being destroyed, but verify "+
				"by hand: a deliberately invalid request should never have succeeded", created.ID))
		if created.ID != "" {
			if derr := r.Destroy(ctx, created.ID); derr != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf(
					"AND THE PROBE POD COULD NOT BE DESTROYED (%v). Pod %s is billing now.", derr, created.ID))
			}
		}
	case strings.Contains(err.Error(), "401"), strings.Contains(err.Error(), "403"):
		rep.MissingPermissions = append(rep.MissingPermissions,
			"pods:write — the API key is read-only or restricted. Provisioning would fail at the first launch.")
	case strings.Contains(err.Error(), "400"), strings.Contains(err.Error(), "422"):
		// Authenticated, body rejected: write capability demonstrated.
	default:
		/*
		 * A Warning, deliberately not Inconclusive. OK requires Inconclusive to
		 * be empty, so a transient 502 recorded there would make a correct
		 * configuration permanently un-OK — which teaches operators the check
		 * is noise, and that is how the one real failure gets clicked through.
		 */
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"the write-capability probe was inconclusive (%v); a read-only key would not be detected "+
				"until the first launch attempt", err))
	}
}

/*
 * ExploreCapacity renders RunPod as a data-centre x GPU-type grid.
 *
 * The richest of the three reports, because lowestPrice returns rentedCount and
 * totalCount: every cell here carries how many units exist and how many are
 * free, which neither AWS nor Vast.ai can express.
 *
 * Data centres are discovered from the operator's configuration rather than
 * enumerated: RunPod's schema exposes no "list all data centres" query, so the
 * unpinned row is what an operator sees until they name one.
 */
func (r *RunPodProvider) ExploreCapacity(ctx context.Context) (*CapacityReport, error) {
	report := &CapacityReport{
		PlacementLabel: "Data center",
		HardwareLabel:  "GPU type",
		Signals:        runpodSignals(),
	}

	dcs := r.settings.DataCenterIDs
	types, err := r.fetchGPUTypes(ctx, dcs)
	if err != nil {
		return nil, fmt.Errorf("runpod: survey GPU types: %w", err)
	}

	// Every card the tier carries, so the operator can widen rather than only
	// confirm what they already picked.
	var hardware []string
	byID := map[string]runpodGPUType{}
	for _, gt := range types {
		if (r.secure() && !gt.SecureCloud) || (!r.secure() && !gt.CommunityCloud) {
			continue
		}
		hardware = append(hardware, gt.ID)
		byID[gt.ID] = gt
	}
	sort.Strings(hardware)
	report.Hardware = hardware

	selectedTypes := lowerSet(r.settings.GPUTypeIDs)

	// The unpinned row first: with no data centre configured this is the whole
	// configuration, and it is the WIDEST option rather than a fallback.
	placements := []struct{ id, name string }{}
	if len(dcs) == 0 {
		placements = append(placements, struct{ id, name string }{"", "Anywhere (RunPod chooses)"})
		report.Warnings = append(report.Warnings,
			"No data centres are configured, which on RunPod is the WIDEST setting, not the narrowest: "+
				"RunPod's own scheduler may place a pod anywhere. Pinning data centres narrows the pool — "+
				"the opposite of how AWS zones behave. Add them for data residency, or to make the launch "+
				"retry loop walk genuinely independent pools.")
	}
	for _, dc := range dcs {
		placements = append(placements, struct{ id, name string }{dc, dc})
	}

	for _, pl := range placements {
		pc := PlacementCapacity{
			ID:   pl.id,
			Name: pl.name,
			Ref:  pl.id,
			// Configured data centres are by definition selected; the unpinned
			// row is selected exactly when nothing is pinned.
			Selected: true,
			Usable:   true,
		}
		if pl.id == "" {
			pc.ID = "any"
		}

		var free, total int
		for _, id := range hardware {
			gt := byID[id]
			lp := gt.LowestPrice
			if pl.id != "" {
				lp = gt.ByDC[pl.id]
			}
			avail := runpodAvailability(lp)

			hc := HardwareCapacity{
				ID: id,
				// Offered means the tier carries the card AND stock is not a
				// definite zero. Unknown counts as offered: no data is not a
				// refusal.
				Offered:  avail != AvailabilityNone,
				Selected: len(selectedTypes) == 0 || selectedTypes[strings.ToLower(id)],
				GPUModel: gt.DisplayName,
				GPUCount: 1,
			}
			if lp != nil {
				if n := lp.TotalCount - lp.RentedCount; n > 0 {
					hc.AvailableCount = n
					free += n
				}
				total += lp.TotalCount
				hc.LiveCents = centsFromDollarsCeil(lp.price(r.settings.Interruptible))
				if lp.TotalCount > 0 {
					hc.Note = fmt.Sprintf("%d/%d rented", lp.RentedCount, lp.TotalCount)
				}
			}
			pc.Hardware = append(pc.Hardware, hc)
			if hc.Selected && hc.Offered {
				report.PoolCount++
			}
		}
		pc.Detail = fmt.Sprintf("%d of %d GPUs free", free, total)
		report.Placements = append(report.Placements, pc)
	}

	if len(report.Hardware) == 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"RunPod reported no GPU types available on the %s tier at all, which is more likely a "+
				"credential or endpoint problem than an empty catalogue", r.cloudType()))
	}
	return report, nil
}

/*
 * runpodSignals declares what RunPod's columns are worth.
 *
 * No configured-price column: RunPod prices live, so an operator-declared rate
 * would be a second source of truth that can only be more wrong. No score
 * either — it does not need one, because it reports the counts a score would be
 * approximating.
 */
func runpodSignals() []SignalDescriptor {
	return []SignalDescriptor{{
		Kind:  SignalOffered,
		Label: "Available",
		Trust: TrustMeasured,
		Explanation: "The tier carries this GPU and RunPod is not reporting zero stock. A blank cell " +
			"means a definite zero; unknown stock still shows as available, because no data is not a refusal.",
	}, {
		Kind:  SignalAvailableCount,
		Label: "GPUs free",
		Trust: TrustMeasured,
		Explanation: "Total units minus rented units, straight from RunPod. Real inventory with the " +
			"denominator included — the most precise availability signal of any provider here.",
	}, {
		Kind:  SignalLivePrice,
		Label: "Per hour",
		Trust: TrustMeasured,
		Explanation: "The lowest live price for one GPU of this type. Note it is the LOWEST, not " +
			"necessarily what you pay: the created pod's actual rate is checked against it at launch and " +
			"any drift over 10% is logged.",
	}}
}
