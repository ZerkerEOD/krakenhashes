/**
 * Helpers for hashcat's $HEX[...] plaintext encoding (GH #90).
 *
 * The backend decodes $HEX[...] cracks into real text whenever the bytes are
 * valid UTF-8. What still reaches the UI as $HEX[...] is a password whose bytes
 * are NOT valid UTF-8 (e.g. $HEX[fe42] from a legacy code page or a ?b mask).
 * Those bytes have no single correct text form, so $HEX[...] stays the exact,
 * hashcat-compatible value and anything else shown is only a best guess.
 */

const HEX_PLAIN_RE = /^\$HEX\[((?:[0-9A-Fa-f]{2})*)\]$/;

/** Returns the raw bytes of a $HEX[...] password, or null for ordinary text. */
export function parseHexPlain(password: string): Uint8Array | null {
  const match = HEX_PLAIN_RE.exec(password);
  if (!match) return null;
  const hex = match[1];
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  }
  return bytes;
}

/**
 * Best-guess readable form of raw password bytes, decoded as Windows-1252 (the
 * most common legacy code page). Control characters are shown as "·" so the
 * preview never contains invisible or line-breaking characters. This is NOT the
 * password: using it as text changes the bytes.
 */
export function hexPlainPreview(bytes: Uint8Array): string {
  const text = new TextDecoder('windows-1252').decode(bytes);
  return Array.from(text)
    .map((ch) => {
      const code = ch.charCodeAt(0);
      return code < 0x20 || code === 0x7f ? '·' : ch;
    })
    .join('');
}

/** Encodes text as $HEX[...] over its UTF-8 bytes. */
function hexEncode(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let hex = '';
  bytes.forEach((b) => {
    hex += b.toString(16).padStart(2, '0');
  });
  return `$HEX[${hex}]`;
}

/**
 * Password as it must appear in a hash:password potfile line. Mirrors the
 * backend export rule (handlers/pot needsHexEncoding): a ':' or control byte
 * would make the line ambiguous, so such passwords are written as $HEX[...].
 * A password that is already $HEX[...] is passed through unchanged.
 */
export function toPotfilePlain(password: string): string {
  if (parseHexPlain(password)) return password;
  for (let i = 0; i < password.length; i++) {
    const code = password.charCodeAt(i);
    if (password[i] === ':' || code < 0x20 || code === 0x7f) {
      return hexEncode(password);
    }
  }
  return password;
}
