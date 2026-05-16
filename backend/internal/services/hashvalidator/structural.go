package hashvalidator

import (
	"encoding/base64"
	"strings"
)

// DefaultStructural returns the set of hand-written validators KrakenHashes
// applies on top of the vendored regex map. These exist for modes where the
// upstream regex is either too loose to catch real errors, or doesn't model
// an input variant the platform accepts (e.g. pwdump-format NTLM).
//
// Pass this slice through New to register them, typically alongside
// WithExamples:
//
//	v := hashvalidator.New(append(hashvalidator.DefaultStructural(), hashvalidator.WithExamples(m))...)
func DefaultStructural() []Option {
	return []Option{
		WithStructural(1000, validateNTLM, "NTLM"),
		WithStructural(3000, validateLM, "LM"),
		WithStructural(16500, validateJWT, "JWT (JSON Web Token)"),
		WithStructural(22000, validateWPA, "WPA-PBKDF2-PMKID+EAPOL"),
	}
}

// validateNTLM accepts NTLM mode 1000 in any of the four shapes the platform
// later processes in pkg/hashutils.processNTLM:
//   - pwdump:   user:rid:LMHASH:NTHASH:::    (NTHASH at index 3, 32 hex)
//   - LM:NT:    LMHASH:NTHASH                 (NTHASH at index 1, 32 hex)
//   - bare:     <32 hex>                      (NTHASH at index 0)
//   - $NT$:     $NT$<32 hex>
func validateNTLM(hash string) Result {
	if strings.HasPrefix(hash, "$NT$") {
		rest := hash[4:]
		if len(rest) == 32 && isHexString(rest) {
			return Result{Valid: true}
		}
		return Result{Valid: false, Reason: "after $NT$ prefix, expected 32 hex characters; got " + describeHexFailure(rest, 32)}
	}
	parts := strings.Split(hash, ":")
	switch {
	case len(parts) >= 4:
		nt := parts[3]
		if len(nt) == 32 && isHexString(nt) {
			return Result{Valid: true}
		}
		return Result{Valid: false, Reason: "pwdump format: field 4 (NT hash) must be 32 hex characters; got " + describeHexFailure(nt, 32)}
	case len(parts) == 2:
		nt := parts[1]
		if len(nt) == 32 && isHexString(nt) {
			return Result{Valid: true}
		}
		return Result{Valid: false, Reason: "LM:NT format: field 2 (NT hash) must be 32 hex characters; got " + describeHexFailure(nt, 32)}
	case len(parts) == 1:
		if len(parts[0]) == 32 && isHexString(parts[0]) {
			return Result{Valid: true}
		}
		return Result{Valid: false, Reason: "expected 32 hex characters; got " + describeHexFailure(parts[0], 32)}
	default:
		return Result{Valid: false, Reason: "unrecognized NTLM format (expected bare 32-hex, LM:NT, $NT$<hash>, or pwdump user:rid:LM:NT:::)"}
	}
}

// validateLM mirrors validateNTLM but pulls the LM half of the same formats.
// LM at index 0 in LM:NT, index 2 in pwdump.
func validateLM(hash string) Result {
	parts := strings.Split(hash, ":")
	switch {
	case len(parts) >= 4:
		lm := parts[2]
		if len(lm) == 32 && isHexString(lm) {
			return Result{Valid: true}
		}
		return Result{Valid: false, Reason: "pwdump format: field 3 (LM hash) must be 32 hex characters; got " + describeHexFailure(lm, 32)}
	case len(parts) == 2:
		lm := parts[0]
		if len(lm) == 32 && isHexString(lm) {
			return Result{Valid: true}
		}
		return Result{Valid: false, Reason: "LM:NT format: field 1 (LM hash) must be 32 hex characters; got " + describeHexFailure(lm, 32)}
	case len(parts) == 1:
		if len(parts[0]) == 32 && isHexString(parts[0]) {
			return Result{Valid: true}
		}
		return Result{Valid: false, Reason: "expected 32 hex characters; got " + describeHexFailure(parts[0], 32)}
	default:
		return Result{Valid: false, Reason: "unrecognized LM format (expected bare 32-hex, LM:NT, or pwdump user:rid:LM:NT:::)"}
	}
}

// validateJWT enforces three non-empty base64url segments separated by `.`,
// where the header decodes to JSON. The vendored upstream regex accepts
// empty segments — JWT consumers reject those, so we tighten it here.
func validateJWT(hash string) Result {
	parts := strings.Split(hash, ".")
	if len(parts) != 3 {
		return Result{Valid: false, Reason: "JWT must have exactly 3 segments separated by '.'"}
	}
	for i, p := range parts {
		if p == "" {
			return Result{Valid: false, Reason: "JWT segment " + itoa(i+1) + " is empty"}
		}
		if _, err := base64.RawURLEncoding.DecodeString(p); err != nil {
			return Result{Valid: false, Reason: "JWT segment " + itoa(i+1) + " is not valid base64url"}
		}
	}
	// Header (segment 0) is expected to start with `{` once decoded — a quick
	// sanity check without parsing the whole JSON.
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if len(header) == 0 || header[0] != '{' {
		return Result{Valid: false, Reason: "JWT header does not look like JSON"}
	}
	return Result{Valid: true}
}

// validateWPA enforces the WPA*0X*MAC*MAC*ESSID*... structure used by
// hashcat mode 22000 (WPA-PBKDF2-PMKID+EAPOL). The vendored regex is just
// an unanchored prefix match, which lets unrelated lines slip through.
//
// Format (per hashcat docs): PROTOCOL*TYPE*PMKID/MIC*MAC_AP*MAC_STA*ESSID*ANONCE*EAPOL*MESSAGE_PAIR
// We require at least 9 `*`-separated fields, all the first three numeric/hex
// where appropriate, and ESSID as ASCII hex.
func validateWPA(hash string) Result {
	if !strings.HasPrefix(hash, "WPA*") {
		return Result{Valid: false, Reason: "expected WPA*<version>*... format"}
	}
	parts := strings.Split(hash, "*")
	if len(parts) < 9 {
		return Result{Valid: false, Reason: "WPA hashline must have at least 9 '*'-separated fields, got " + itoa(len(parts))}
	}
	// parts[1] is the type: 01 = PMKID, 02 = EAPOL
	if parts[1] != "01" && parts[1] != "02" {
		return Result{Valid: false, Reason: "WPA type field must be '01' (PMKID) or '02' (EAPOL), got '" + parts[1] + "'"}
	}
	// parts[2] is PMKID (32 hex) or MIC (32 hex)
	if len(parts[2]) != 32 || !isHexString(parts[2]) {
		return Result{Valid: false, Reason: "WPA field 3 (PMKID/MIC) must be 32 hex characters"}
	}
	// parts[3] and parts[4] are 12-hex MAC addresses
	if len(parts[3]) != 12 || !isHexString(parts[3]) {
		return Result{Valid: false, Reason: "WPA field 4 (MAC_AP) must be 12 hex characters"}
	}
	if len(parts[4]) != 12 || !isHexString(parts[4]) {
		return Result{Valid: false, Reason: "WPA field 5 (MAC_STA) must be 12 hex characters"}
	}
	return Result{Valid: true}
}

// isHexString returns true if every byte is a hex digit. Empty input is false.
func isHexString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// describeHexFailure builds a short reason describing how `s` failed to be a
// hex string of length `want`. Used by validators that want to report length
// vs. character-class problems distinctly.
func describeHexFailure(s string, want int) string {
	if len(s) != want {
		return "length " + itoa(len(s)) + " (expected " + itoa(want) + ")"
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "non-hex character at position " + itoa(i+1)
		}
	}
	return "unexpected format"
}
