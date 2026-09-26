package errorclass

import "testing"

/*
 * GH #91: an agent given work with no hashcat binary path rejects it with the
 * typed code TASK_NO_BINARY. Its callers wrap that error in text that also
 * matches the not-ready markers ("failed to ensure/resolve hashcat binary"), so
 * the typed code must win: not-ready would retry forever, and transient would
 * count toward the agent's blocklist threshold for a fault that is not the
 * agent's.
 */
func TestClassify_TaskNoBinary(t *testing.T) {
	for _, msg := range []string{
		"failed to ensure hashcat binary: TASK_NO_BINARY: no hashcat binary is assigned to this task (empty binary path)",
		"failed to build command: failed to resolve hashcat binary: TASK_NO_BINARY: no hashcat binary is assigned to this task (empty binary path)",
	} {
		got := Classify(msg)
		if got != CategoryJobConfig {
			t.Errorf("Classify(%q) = %q, want %q", msg, got, CategoryJobConfig)
		}
		if got.IsNotReady() {
			t.Errorf("TASK_NO_BINARY must not be not-ready (would retry forever): %q", msg)
		}
		if got.IsTransient() {
			t.Errorf("TASK_NO_BINARY must not be transient (would count toward the agent blocklist): %q", msg)
		}
	}
}
