// Package bloodhound parses BloodHound / SharpHound Active Directory collection dumps entirely
// in memory and resolves a compact per-account AD-privilege fact set (DerivedContext) used to
// enrich KrakenHashes analytics reports.
//
// INVARIANT: this package never writes the raw dump (or anything derived from it) to disk. It
// operates only on io.Reader / bytes buffers. There are intentionally NO os.Create / os.CreateTemp
// / os.MkdirAll calls anywhere in this package. The raw graph is discarded once Resolve() returns;
// only the small DerivedContext is handed back to the caller for transient staging.
package bloodhound

import "errors"

// ErrTooLarge is returned when an input exceeds the configured size/zip-bomb limits.
var ErrTooLarge = errors.New("bloodhound: input exceeds configured size limits")

// Limits bounds resource use while parsing an untrusted BloodHound dump. All byte limits are hard
// caps; object/edge limits degrade gracefully (past MaxObjects extra objects are dropped; past
// MaxEdges attack-path analysis is skipped but privilege facts are still produced).
type Limits struct {
	MaxRequestBytes      int64 // whole multipart request body (enforced by the handler via http.MaxBytesReader)
	MaxZipBytes          int64 // a buffered .zip (archive/zip needs random access, so it must fit in memory)
	MaxEntryBytes        int64 // a single decompressed document (zip entry or json part)
	MaxTotalDecompressed int64 // sum of all decompressed bytes across a zip (zip-bomb guard)
	MaxObjects           int   // total users+groups+computers ingested
	MaxEdges             int   // attack-graph edges; past this, path-to-DA is skipped
	MaxUsersForDomainAgg int   // above this many users, skip closure-based domain denominators
}

// DefaultLimits returns conservative defaults suitable for typical engagement-sized dumps.
func DefaultLimits() Limits {
	return Limits{
		MaxRequestBytes:      512 << 20,  // 512 MiB request
		MaxZipBytes:          256 << 20,  // 256 MiB zip buffered in memory
		MaxEntryBytes:        1 << 30,    // 1 GiB per decompressed document
		MaxTotalDecompressed: 4 << 30,    // 4 GiB total decompressed
		MaxObjects:           2_000_000,  // users+groups+computers
		MaxEdges:             10_000_000, // attack-graph edges
		MaxUsersForDomainAgg: 1_000_000,  // closure-based domain denominators
	}
}
