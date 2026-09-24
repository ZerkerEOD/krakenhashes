package services

import "testing"

// forgetBinary clears cached state so subtests do not leak into each other --
// the cache is deliberately process-global.
func forgetBinary(t *testing.T, path string) {
	t.Helper()
	totalCandidatesUnsupported.Delete(path)
	t.Cleanup(func() { totalCandidatesUnsupported.Delete(path) })
}

func TestTotalCandidatesCapabilityCache(t *testing.T) {
	// The exact text hashcat 6.2.6 prints, taken from a real deployment's logs.
	const realStderr = "/var/lib/krakenhashes/binaries/local/1/hashcat.bin: unrecognized option '--total-candidates'"

	t.Run("an unknown binary is tried", func(t *testing.T) {
		path := "/binaries/local/99/hashcat.bin"
		forgetBinary(t, path)

		if !hashcatSupportsTotalCandidates(path) {
			t.Error("a binary with no recorded failure should be attempted")
		}
	})

	t.Run("the unsupported-flag failure is recognised and remembered", func(t *testing.T) {
		path := "/binaries/local/1/hashcat.bin"
		forgetBinary(t, path)

		if !noteTotalCandidatesFailure(path, realStderr) {
			t.Fatal("noteTotalCandidatesFailure = false for an unrecognized-option stderr")
		}
		if hashcatSupportsTotalCandidates(path) {
			t.Error("binary still reported as supporting the flag after it rejected it")
		}
		// Repeating must stay true and must not re-warn (idempotent).
		if !noteTotalCandidatesFailure(path, realStderr) {
			t.Error("second call = false; the verdict should be stable")
		}
	})

	// Every other failure must fall through to the existing generic handling.
	// Treating a timeout or a busy instance as "flag unsupported" would disable
	// exact keyspace permanently on a binary that supports it fine.
	t.Run("unrelated failures are not treated as unsupported", func(t *testing.T) {
		cases := map[string]string{
			"busy instance":  "Already an instance of hashcat running",
			"missing device": "No devices found/left",
			"empty":          "",
			"other flag":     "hashcat.bin: unrecognized option '--some-other-flag'",
		}
		for name, stderr := range cases {
			t.Run(name, func(t *testing.T) {
				path := "/binaries/local/unrelated-" + name + "/hashcat.bin"
				forgetBinary(t, path)

				if noteTotalCandidatesFailure(path, stderr) {
					t.Errorf("stderr %q was misread as an unsupported flag", stderr)
				}
				if !hashcatSupportsTotalCandidates(path) {
					t.Error("binary was disabled by an unrelated failure")
				}
			})
		}
	})

	// Uploading a new hashcat creates a new binary id and therefore a new path,
	// so one rejecting binary must never disable another.
	t.Run("the verdict is per binary", func(t *testing.T) {
		old := "/binaries/local/1/hashcat.bin"
		upgraded := "/binaries/local/2/hashcat.bin"
		forgetBinary(t, old)
		forgetBinary(t, upgraded)

		noteTotalCandidatesFailure(old, realStderr)

		if hashcatSupportsTotalCandidates(old) {
			t.Error("the rejecting binary should be disabled")
		}
		if !hashcatSupportsTotalCandidates(upgraded) {
			t.Error("a different binary was disabled by its neighbour's failure")
		}
	})
}
