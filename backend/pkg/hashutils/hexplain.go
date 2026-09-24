package hashutils

import (
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// DecodeHexPlain turns hashcat's $HEX[...] plaintext encoding back into the
// real password (GH #90).
//
// hashcat's outfile hex-encodes any plain containing the separator (':') or a
// non-printable byte, so a cracked "Pass:word" arrives as
// "$HEX[506173733a776f7264]". Stored verbatim, that literal skews analytics and
// shows garbage in the UI.
//
// The decoded value is used only when it is safe to store as plain text:
// valid UTF-8 with no NUL, '\n' or '\r' (Postgres TEXT cannot hold NUL, and
// line breaks would corrupt line-oriented potfiles and exports). Anything else
// keeps the literal $HEX[...], which hashcat itself reads back natively.
//
// Only a string that is entirely one $HEX[...] token is decoded, so an
// already-decoded password that merely contains "$HEX[" is left alone.
func DecodeHexPlain(plain string) string {
	const prefix, suffix = "$HEX[", "]"
	if len(plain) < len(prefix)+len(suffix) ||
		!strings.HasPrefix(plain, prefix) || !strings.HasSuffix(plain, suffix) {
		return plain
	}

	inner := plain[len(prefix) : len(plain)-len(suffix)]
	decoded, err := hex.DecodeString(inner)
	if err != nil {
		return plain // odd length or non-hex: not a real $HEX[] token
	}
	if !utf8.Valid(decoded) || strings.ContainsAny(string(decoded), "\x00\n\r") {
		return plain
	}
	return string(decoded)
}
