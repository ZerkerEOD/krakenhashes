package hashvalidator

import (
	"regexp"
	"strings"
)

// deriveExamplePattern attempts to build a simple character-class regex from
// a hash_types.example string. Returns nil when the example is ambiguous,
// structurally complex, or empty — callers fall through to Unvalidated.
//
// The heuristic intentionally bails on examples with structural separators
// (`$`, `:`, `*`, etc.) because guessing at multi-segment formats risks
// silent acceptance of malformed hashes. For those modes we'd rather report
// Unvalidated and surface the "no known validator" notice to the user.
func deriveExamplePattern(example string) *regexp.Regexp {
	example = strings.TrimSpace(example)
	if example == "" || len(example) > 4096 {
		return nil
	}
	if hasStructuralChars(example) {
		return nil
	}
	cls := classifyCharClass(example)
	if cls == "" {
		return nil
	}
	pattern := "(?i)^[" + cls + "]{" + itoa(len(example)) + "}$"
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

func hasStructuralChars(s string) bool {
	return strings.ContainsAny(s, "$:*|()\\<>?\"',; \t\n")
}

func classifyCharClass(s string) string {
	allHex := true
	allAlphanum := true
	allBase64 := true
	for _, r := range s {
		if !isHexRune(r) {
			allHex = false
		}
		if !isAlphanumRune(r) {
			allAlphanum = false
		}
		if !isBase64Rune(r) {
			allBase64 = false
		}
	}
	switch {
	case allHex:
		return "a-fA-F0-9"
	case allAlphanum:
		return "a-zA-Z0-9"
	case allBase64:
		return `a-zA-Z0-9+/=`
	default:
		return ""
	}
}

func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func isAlphanumRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isBase64Rune(r rune) bool {
	return isAlphanumRune(r) || r == '+' || r == '/' || r == '='
}
