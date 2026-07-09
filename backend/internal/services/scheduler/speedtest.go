package scheduler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
)

// compressedWordlistExts are the wordlist file suffixes we treat as compressed
// when picking a benchmark timeout. Hashcat needs significantly more time to
// dictstat-preprocess these before the GPUs start crunching, so they get the
// larger configured timeout. Match is case-insensitive against the trailing
// suffix of the wordlist path.
//
// This set must mirror the formats hashcat itself recognises in
// src/filehandling.c (magic-byte detection at hc_fopen):
//   - .gz  → gzip (magic 1F 8B 08), opened via gzdopen
//   - .zip → zip  (magic PK\x03\x04), opened via unzOpen64 (first file in archive)
//   - .xz  → xz   (magic FD 37 7A 58 5A 00), opened via the LZMA-SDK xz unpacker
//
// Hashcat does not support bzip2, zstd, or 7z wordlists — adding those
// extensions would just hand them the longer timeout and then surface a
// typed read failure, with no chance of success.
var compressedWordlistExts = []string{".gz", ".zip", ".xz"}

// HasCompressedWordlist returns true if any of the supplied wordlist paths
// looks compressed by extension. Shared by the scheduler-v2 benchmark dispatch
// and the legacy integration speed-test path so both pick the same timeout.
func HasCompressedWordlist(paths []string) bool {
	for _, p := range paths {
		lower := strings.ToLower(p)
		for _, ext := range compressedWordlistExts {
			if strings.HasSuffix(lower, ext) {
				return true
			}
		}
	}
	return false
}

// ResolveSpeedTestParameters computes the (testDuration, timeoutDuration,
// minStatusUpdates) triple for a BenchmarkRequestPayload from the speed-test
// admin settings. getInt reads a system_setting as an int, returning
// (value, true) on success and (0, false) when the setting is missing/invalid.
//
// If a compressed wordlist is in the request the larger compressed timeout
// wins. The wall-clock TimeoutDuration is set slightly larger than TestDuration
// so the agent's outer context never fires before the inner status-collection
// deadline. Falls back to conservative defaults if any key is missing, so a
// partial DB still runs.
//
// This is the single source of truth: both
// JobWebSocketIntegration.resolveSpeedTestParameters and the scheduler-v2
// buildBenchmarkRequest call it.
func ResolveSpeedTestParameters(getInt func(key string) (int, bool), wordlistPaths []string) (testDuration, timeoutDuration, minStatusUpdates int) {
	// Conservative defaults that match the migration seeds.
	const (
		defaultUncompressed = 120
		defaultCompressed   = 300
		defaultMinUpdates   = 3
		timeoutGraceSeconds = 60
	)

	uncompressed := defaultUncompressed
	compressed := defaultCompressed
	minStatusUpdates = defaultMinUpdates

	if getInt != nil {
		if v, ok := getInt("speed_test_timeout_seconds_uncompressed"); ok && v > 0 {
			uncompressed = v
		}
		if v, ok := getInt("speed_test_timeout_seconds_compressed"); ok && v > 0 {
			compressed = v
		}
		if v, ok := getInt("speed_test_min_status_updates"); ok && v >= 1 {
			minStatusUpdates = v
		}
	}

	if HasCompressedWordlist(wordlistPaths) {
		testDuration = compressed
	} else {
		testDuration = uncompressed
	}

	timeoutDuration = testDuration + timeoutGraceSeconds

	return testDuration, timeoutDuration, minStatusUpdates
}

// dbIntSettingReader returns a getInt closure for ResolveSpeedTestParameters
// that reads system_settings directly from the database. Used by the
// scheduler-v2 benchmark dispatch path, which only has a *db.DB handle.
func dbIntSettingReader(ctx context.Context, database *db.DB) func(string) (int, bool) {
	return func(key string) (int, bool) {
		if database == nil {
			return 0, false
		}
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var value string
		err := database.QueryRowContext(readCtx,
			`SELECT value FROM system_settings WHERE key = $1`, key).Scan(&value)
		if err != nil {
			return 0, false
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}
