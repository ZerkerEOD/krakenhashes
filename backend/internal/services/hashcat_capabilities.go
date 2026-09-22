package services

import (
	"strings"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
Which optional hashcat flags a given binary actually understands.

--total-candidates arrived in hashcat 7. A deployment running 6.2.6 rejects it
with exit status 255 before doing any work, and every keyspace pre-flight falls
back to the estimator -- correct, but it re-ran the doomed exec on a timer and
logged a warning each time. On the reference deployment that was one WARNING
every ten minutes, indefinitely, for a condition that cannot change until the
operator uploads a different binary.

Cached per binary PATH rather than per version string, because the path is what
the exec actually uses and it already encodes identity: binaries live at
<data>/binaries/local/<binary_version_id>/hashcat.bin, and uploading a new
hashcat creates a new id and therefore a new path. Replacing the bytes at an
existing path -- which the application never does itself -- would leave this
stale until the next restart.

Learned from a real failure rather than probed up front: the first attempt costs
one exec that was going to run anyway, and nothing has to guess at version
parsing.
*/

// totalCandidatesUnsupported holds the set of hashcat binary paths known to
// reject --total-candidates. Values are struct{}; only key presence matters.
var totalCandidatesUnsupported sync.Map

// totalCandidatesUnsupportedMarker is what hashcat's argument parser prints for
// an unknown long option, e.g.
// "/var/lib/krakenhashes/binaries/local/1/hashcat.bin: unrecognized option '--total-candidates'"
const totalCandidatesUnsupportedMarker = "unrecognized option '--total-candidates'"

// hashcatSupportsTotalCandidates reports whether the binary at path is worth
// invoking with --total-candidates. False only once a previous run has proven
// otherwise, so an unknown binary is always tried.
func hashcatSupportsTotalCandidates(binaryPath string) bool {
	_, unsupported := totalCandidatesUnsupported.Load(binaryPath)
	return !unsupported
}

// noteTotalCandidatesFailure inspects a failed run's stderr and reports whether
// it failed because the flag does not exist.
//
// When it did, the binary is recorded so later callers skip the exec, and the
// explanation is logged ONCE per binary rather than on every attempt. Returning
// true means the caller should fall back to the estimator without logging the
// failure again.
func noteTotalCandidatesFailure(binaryPath, stderr string) bool {
	if !strings.Contains(stderr, totalCandidatesUnsupportedMarker) {
		return false
	}

	if _, alreadyKnown := totalCandidatesUnsupported.LoadOrStore(binaryPath, struct{}{}); !alreadyKnown {
		debug.Warning("hashcat at %s does not support --total-candidates (added in hashcat 7); "+
			"keyspace will be estimated for every job using this binary. Upload a hashcat 7.x "+
			"binary for exact effective keyspace. This is logged once per binary.", binaryPath)
	}

	return true
}
