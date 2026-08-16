package cloud

import (
	"context"
	"errors"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// ErrOfferUnavailable means the chosen capacity vanished between search and
// launch. Callers should try the next offer, never retry the same one.
var ErrOfferUnavailable = errors.New("cloud offer no longer available")

// ErrNotFound means the provider has no record of an instance. For teardown
// this is success: it cannot bill for something that does not exist.
var ErrNotFound = errors.New("cloud instance not found at provider")

// Offer is a rentable unit of capacity.
type Offer struct {
	// ID is provider-scoped: a Vast.ai ask id, an AWS instance type.
	ID              string
	InstanceType    string
	GPUModel        string
	GPUCount        int
	HourlyRateCents int
	// StorageCentsPerHour and BandwidthCentsPerGB are billed separately by
	// Vast.ai and are easy to forget; they feed the reservation's extra term.
	StorageCentsPerHour int
	BandwidthCentsPerGB int
	Region              string
	// MaxDuration is how long the provider guarantees the capacity. Zero means
	// unbounded. Filter on this: an offer that expires mid-job is wasted spend.
	MaxDuration time.Duration
	Raw         models.JSONMap
}

// LaunchRequest is everything a provider needs to start one instance.
type LaunchRequest struct {
	// Label is written to our database BEFORE this call and tagged onto the
	// instance. It is how a lost response is reconciled, and on Vast.ai — which
	// has no idempotency token — it is the idempotency key.
	Label string
	// IdempotencyKey is deterministic per instance so an SDK-level retry after
	// a network timeout cannot double-launch. Never a fresh random per attempt.
	IdempotencyKey string

	Offer  Offer
	DiskGB int

	// TTL bounds the instance's life. Armed BOTH provider-side where possible
	// and inside the guest, because only the in-guest timer survives the
	// backend disappearing.
	TTL time.Duration

	// Env is injected into the agent container: KH_HOST, KH_CLAIM_CODE,
	// KH_EPHEMERAL, the VPN provider and its credential, and the deadline.
	Env map[string]string

	// Image is the agent container image.
	Image string
}

// LaunchResult is what the provider returns once capacity is committed.
type LaunchResult struct {
	ProviderInstanceID string
	LaunchedAt         time.Time
	// BilledFrom is when the provider STARTED CHARGING, which is not the same
	// as when the container became ready. Vast.ai bills storage from contract
	// creation. Accrual must use this, or spend is systematically under-reported.
	BilledFrom time.Time
	Raw        models.JSONMap
}

// InstanceStatus is a provider-side observation.
type InstanceStatus struct {
	ProviderInstanceID string
	// State is normalized across providers.
	State InstanceObservedState
	// Terminal means this instance will never become usable. Vast.ai's
	// exited/unknown/offline never recover — polling them is burning money.
	Terminal bool
	Message  string
	Raw      models.JSONMap
}

// InstanceObservedState normalizes provider status vocabularies.
type InstanceObservedState string

const (
	ObservedPending InstanceObservedState = "pending"
	ObservedRunning InstanceObservedState = "running"
	ObservedStopped InstanceObservedState = "stopped"
	ObservedGone    InstanceObservedState = "gone"
	ObservedError   InstanceObservedState = "error"
)

/*
 * Provider is one cloud GPU backend.
 *
 * Every method must be safe to call repeatedly. In particular Destroy must be
 * idempotent and must treat "already gone" as success: the reaper will call it
 * again on anything it is not certain about, and refusing to converge is how
 * an instance bills forever.
 */
type Provider interface {
	// Name identifies the provider for logs and errors.
	Name() models.CloudProvider

	// Preflight verifies credentials, permissions and quota BEFORE any money
	// can be spent, and reports precisely what is missing. Run at config time
	// and again before the first launch of a session.
	Preflight(ctx context.Context) (*PreflightReport, error)

	// SearchOffers returns candidate capacity, cheapest-viable first.
	SearchOffers(ctx context.Context, req OfferQuery) ([]Offer, error)

	// Launch starts one instance. Implementations must tag/label it with
	// req.Label so ListOwned can find it even if this call's response is lost.
	Launch(ctx context.Context, req LaunchRequest) (*LaunchResult, error)

	// Status polls one instance.
	Status(ctx context.Context, providerInstanceID string) (*InstanceStatus, error)

	// ListOwned returns every instance the provider believes belongs to us,
	// keyed by label. This is the orphan-detection primitive: anything here
	// without a matching database row is billing with nobody accounting for it.
	ListOwned(ctx context.Context) (map[string]InstanceStatus, error)

	// Destroy terminates an instance. MUST be idempotent and MUST return nil
	// (not ErrNotFound) when the instance is already gone.
	Destroy(ctx context.Context, providerInstanceID string) error

	// CostSoFar returns provider-authoritative spend in cents when available.
	// Returning (0, false, nil) means "no authoritative figure yet" and the
	// caller should keep using its own wall-clock estimate.
	CostSoFar(ctx context.Context, providerInstanceID string) (cents int64, authoritative bool, err error)
}

// OfferQuery constrains a capacity search.
type OfferQuery struct {
	MinGPUCount        int
	MaxHourlyRateCents int
	MinDiskGB          int
	// MinDuration filters out capacity that expires before the job could
	// plausibly finish.
	MinDuration time.Duration
	// AllowedInstanceTypes restricts to an operator-approved list.
	AllowedInstanceTypes []string
	// VerifiedOnly asks for the provider's trusted tier where it has one.
	VerifiedOnly bool
	Limit        int
}

// PreflightReport is a provider's honest self-assessment.
type PreflightReport struct {
	OK       bool   `json:"ok"`
	Identity string `json:"identity,omitempty"`
	// MissingPermissions names what is denied, so the operator gets an
	// actionable list rather than a bare "access denied".
	MissingPermissions []string `json:"missing_permissions,omitempty"`
	// Inconclusive lists checks that could not be decided. Treated as failure:
	// "unknown" is not "allowed".
	Inconclusive []string `json:"inconclusive,omitempty"`
	// QuotaLimit / QuotaUsed are in provider-native units (vCPUs on AWS).
	QuotaLimit  float64  `json:"quota_limit,omitempty"`
	QuotaUsed   float64  `json:"quota_used,omitempty"`
	QuotaSource string   `json:"quota_source,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}
