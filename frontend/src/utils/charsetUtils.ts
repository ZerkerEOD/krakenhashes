// Built-in hashcat charset sizes
const BUILTIN_CHARSET_SIZES: Record<string, number> = {
  '?l': 26,  // lowercase a-z
  '?u': 26,  // uppercase A-Z
  '?d': 10,  // digits 0-9
  '?s': 33,  // special chars
  '?a': 95,  // all printable ASCII
  '?b': 256, // all bytes
  '?h': 16,  // lowercase hex 0-9a-f
  '?H': 16,  // uppercase hex 0-9A-F
};

/**
 * Resolves the number of unique characters in a custom charset definition.
 * Handles built-in placeholders, references to earlier custom charsets, and literals.
 */
export function resolveCharsetSize(
  definition: string,
  customCharsets: Record<string, string>,
  resolved: Record<string, number>
): number {
  if (!definition) return 0;

  let totalSize = 0;
  const uniqueLiterals = new Set<string>();
  let i = 0;

  while (i < definition.length) {
    if (definition[i] === '?' && i + 1 < definition.length) {
      const placeholder = definition.substring(i, i + 2);
      const second = definition[i + 1];

      if (BUILTIN_CHARSET_SIZES[placeholder] !== undefined) {
        totalSize += BUILTIN_CHARSET_SIZES[placeholder];
      } else if (second >= '1' && second <= '4') {
        const slot = second;
        if (resolved[slot] !== undefined) {
          totalSize += resolved[slot];
        }
        // Skip unresolved references (forward refs not supported)
      }
      i += 2;
    } else {
      if (!uniqueLiterals.has(definition[i])) {
        uniqueLiterals.add(definition[i]);
        totalSize++;
      }
      i++;
    }
  }

  return totalSize;
}

/**
 * Calculates the estimated keyspace for a mask with custom charsets.
 * Returns 0 if the mask is empty.
 */
export function calculateMaskKeyspace(
  mask: string,
  customCharsets: Record<string, string>
): number {
  if (!mask) return 0;

  // Pre-resolve custom charset sizes (ordered 1-4 for back-references)
  const resolved: Record<string, number> = {};
  for (const slot of ['1', '2', '3', '4']) {
    const def = customCharsets[slot];
    if (def) {
      resolved[slot] = resolveCharsetSize(def, customCharsets, resolved);
    }
  }

  let keyspace = 1;
  let i = 0;

  while (i < mask.length) {
    if (mask[i] === '?' && i + 1 < mask.length) {
      const placeholder = mask.substring(i, i + 2);
      const second = mask[i + 1];

      if (BUILTIN_CHARSET_SIZES[placeholder] !== undefined) {
        keyspace *= BUILTIN_CHARSET_SIZES[placeholder];
      } else if (second >= '1' && second <= '4') {
        const size = resolved[second] || 26; // fallback to 26 if undefined
        keyspace *= size;
      }
      i += 2;
    } else {
      // Literal - doesn't multiply keyspace
      i++;
    }
  }

  return keyspace;
}

/**
 * Formats a large number with commas for display.
 */
export function formatKeyspace(keyspace: number): string {
  if (keyspace === 0) return '0';
  return keyspace.toLocaleString();
}

/**
 * Validates a charset definition string.
 * Returns an error message or empty string if valid.
 */
export function validateCharsetDefinition(definition: string): string {
  if (!definition) return 'Definition cannot be empty';

  let i = 0;
  while (i < definition.length) {
    if (definition[i] === '?' && i + 1 < definition.length) {
      const second = definition[i + 1];
      const valid = 'ludsabhH1234';
      if (!valid.includes(second)) {
        return `Invalid placeholder ?${second}`;
      }
      i += 2;
    } else if (definition[i] === '?' && i + 1 >= definition.length) {
      return 'Incomplete placeholder at end';
    } else {
      i++;
    }
  }

  return '';
}
