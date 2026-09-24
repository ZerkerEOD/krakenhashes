package cloud

import (
	"os"
	"path/filepath"
	"strings"
)

/*
 * Helpers for STRUCTURAL guards — tests that read this package's own source.
 *
 * Used sparingly and only where the property is "this branch must NOT call X".
 * That is genuinely awkward to assert behaviourally: proving a call did not
 * happen on one of three error paths means standing up a Service with a
 * database, a budget engine, a VPN minter and a fake provider, and then driving
 * it into each failure in turn. The setup would be larger than the code it
 * guards and would itself need trusting.
 *
 * BE HONEST ABOUT WHAT THESE PROVE. A structural guard catches the regression it
 * names — someone adding killVoucher to the ambiguous branch — and nothing else.
 * It cannot tell you the code works. The behavioural coverage for the voucher
 * lifecycle is scripts/cloud-e2e-test.sh, which drives a real provisioning cycle
 * end to end. If one of these guards ever starts failing for a reason that is
 * not the regression in its message, delete it rather than reshaping it.
 */

func readSource(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readServiceSource() (string, error) { return readSource("service.go") }

// extractBetween returns the text from the first occurrence of `from` up to the
// next `to`, or "" if either is missing. Callers must t.Fatal on "" — an empty
// result means the anchors moved and the guard has quietly become vacuous.
func extractBetween(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	rest := s[i+len(from):]
	j := strings.Index(rest, to)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func indexOf(s, sub string) int { return strings.Index(s, sub) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
