package errorclass

import "testing"

/*
 * The exact string a production agent sent, character for character, from the
 * AWS instance that was blocklisted for 24 hours.
 *
 * Kept verbatim rather than paraphrased: this classification exists solely
 * because that message matched no marker, fell through to Unknown, counted as a
 * transient failure and tripped the threshold three times in eleven seconds. A
 * prettified approximation would not prove the real one is handled.
 */
const productionNotReadyMessage = "failed to build command: failed to resolve hashcat binary: " +
	"hashcat binary not found in directory /app/data/binaries/5. " +
	"Checked paths: [/app/data/binaries/5/hashcat.bin /app/data/binaries/5/hashcat]"

func TestClassify_ProductionNotReadyMessage(t *testing.T) {
	got := Classify(productionNotReadyMessage)
	if got != CategoryAgentNotReady {
		t.Fatalf("Classify(production message) = %q, want %q — this exact string caused a "+
			"24h blocklist on a healthy agent", got, CategoryAgentNotReady)
	}
	if got.IsTransient() {
		t.Error("not-ready must not report IsTransient: transient failures are COUNTED, " +
			"and counting a non-attempt is what tripped the threshold")
	}
	if !got.IsNotReady() {
		t.Error("IsNotReady must be true; it is what AttributeBenchmarkFailure returns on")
	}
}

func TestClassify_NotReadyVariants(t *testing.T) {
	for _, msg := range []string{
		"AGENT_NOT_PROVISIONED",
		"agent not provisioned for this job: failed to ensure hashcat binary: connection reset",
		"hashcat archive found at /app/data/binaries/5/hashcat-7.1.2+338.7z but not extracted",
	} {
		if got := Classify(msg); got != CategoryAgentNotReady {
			t.Errorf("Classify(%q) = %q, want %q", msg, got, CategoryAgentNotReady)
		}
	}
}

/*
 * Guards the precedence decision. A genuinely broken agent and a genuinely bad
 * hashlist must still classify as before — if "not ready" swallowed those, it
 * would suppress the blocklist that legitimately protects a job from an agent
 * that cannot run it.
 */
func TestClassify_NotReadyDoesNotSwallowRealFaults(t *testing.T) {
	cases := map[string]Category{
		"no hashes loaded":                      CategoryHashlistFatal,
		"clGetDeviceIDs(): CL_DEVICE_NOT_FOUND": CategoryAgentPersistent,
		"Benchmark timed out waiting for agent response": CategoryUnknown,
	}
	for msg, want := range cases {
		if got := Classify(msg); got != want {
			t.Errorf("Classify(%q) = %q, want %q", msg, got, want)
		}
	}
}
