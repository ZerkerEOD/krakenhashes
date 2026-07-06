// Package pdf renders completed analytics reports as PDF documents in two
// classifications: Internal (full data, incl. plaintext credentials) and
// External (aggregate-only, server-side redacted).
package pdf

import (
	"encoding/json"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// BuildExternalAnalytics returns an aggregate-only copy of the analytics data
// safe for an externally-distributed document. It deep-copies the input (via a
// JSON round-trip, so the caller's cached struct is never mutated) and then
// strips every leaf that identifies an individual or reveals a literal
// credential: plaintext passwords, usernames, hash values, example values, and
// the common base words extracted from cracked passwords (a base word can itself
// be a recovered password). All aggregate statistics (counts, percentages,
// distributions, domain breakdowns, mask patterns, recommendations) are preserved.
//
// This is the single server-side enforcement point for external sensitivity:
// the external PDF is always built from this output, never from raw data, so the
// browser never receives sensitive data for the external document. The strip is
// applied to the top-level report AND recursively to every per-domain section.
//
// It fails closed: if the deep copy fails, an empty AnalyticsData is returned
// rather than risking a leak of the original.
func BuildExternalAnalytics(in *models.AnalyticsData) *models.AnalyticsData {
	if in == nil {
		return nil
	}

	b, err := json.Marshal(in)
	if err != nil {
		return &models.AnalyticsData{}
	}
	out := &models.AnalyticsData{}
	if err := json.Unmarshal(b, out); err != nil {
		return &models.AnalyticsData{}
	}

	redactAnalytics(out)
	for i := range out.DomainAnalytics {
		redactDomainAnalytics(&out.DomainAnalytics[i])
	}
	return out
}

// redactAnalytics strips sensitive leaves from the top-level report while
// keeping the aggregate counts of each affected section.
func redactAnalytics(a *models.AnalyticsData) {
	a.TopPasswords = nil                     // list of literal plaintext passwords
	a.PatternDetection.CommonBaseWords = nil // base words can equal recovered passwords
	redactReuse(&a.PasswordReuse)
	redactHashReuse(&a.HashReuse)
	redactMasks(&a.MaskAnalysis)
	redactLMPartial(a.LMPartialCracks)
	redactLMToNTLM(a.LMToNTLMMasks)
}

// redactDomainAnalytics applies the same stripping to a per-domain section.
func redactDomainAnalytics(d *models.DomainAnalytics) {
	d.TopPasswords = nil
	d.PatternDetection.CommonBaseWords = nil
	redactReuse(&d.PasswordReuse)
	redactHashReuse(d.HashReuse) // pointer on the domain struct; nil-guarded
	redactMasks(&d.MaskAnalysis)
	redactLMPartial(d.LMPartialCracks)
	redactLMToNTLM(d.LMToNTLMMasks)
}

// redactReuse drops the per-password/per-user detail list, keeping totals.
func redactReuse(r *models.ReuseStats) {
	r.PasswordReuseInfo = nil
}

// redactHashReuse drops the per-hash/per-user detail list, keeping totals.
func redactHashReuse(h *models.HashReuseStats) {
	if h == nil {
		return
	}
	h.HashReuseInfo = nil
}

// redactMasks clears example passwords while keeping the mask patterns/counts.
func redactMasks(m *models.MaskStats) {
	for i := range m.TopMasks {
		m.TopMasks[i].Example = ""
	}
}

// redactLMPartial drops the per-account partial-crack detail list, keeping totals.
func redactLMPartial(l *models.LMPartialCrackStats) {
	if l == nil {
		return
	}
	l.PartialCrackDetails = nil
}

// redactLMToNTLM clears example LM passwords while keeping mask patterns/keyspace.
func redactLMToNTLM(l *models.LMToNTLMMaskStats) {
	if l == nil {
		return
	}
	for i := range l.Masks {
		l.Masks[i].ExampleLM = ""
	}
}
