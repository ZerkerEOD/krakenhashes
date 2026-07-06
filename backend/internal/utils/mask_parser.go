package utils

import (
	"fmt"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// MaskPosition represents a single position in a hashcat mask
type MaskPosition struct {
	Placeholder string // e.g., "?l", "?u", "?d", "?1", or a literal character
	IsLiteral   bool   // true if this is a literal character, false if it's a placeholder
}

// ParseMask parses a hashcat mask into individual positions
// Hashcat placeholders are 2 characters: ?l, ?u, ?d, ?s, ?a, ?b, ?1-?4
// Everything else is treated as a literal character
func ParseMask(mask string) ([]MaskPosition, error) {
	if mask == "" {
		return nil, fmt.Errorf("mask cannot be empty")
	}

	var positions []MaskPosition
	i := 0

	for i < len(mask) {
		if mask[i] == '?' {
			// Check if there's a next character
			if i+1 >= len(mask) {
				return nil, fmt.Errorf("incomplete placeholder at end of mask")
			}

			// Get the placeholder (2 characters)
			placeholder := mask[i : i+2]

			// Validate placeholder
			if !isValidPlaceholder(placeholder) {
				return nil, fmt.Errorf("invalid placeholder: %s", placeholder)
			}

			positions = append(positions, MaskPosition{
				Placeholder: placeholder,
				IsLiteral:   false,
			})

			i += 2 // Skip both characters of the placeholder
		} else {
			// Literal character
			positions = append(positions, MaskPosition{
				Placeholder: string(mask[i]),
				IsLiteral:   true,
			})
			i++
		}
	}

	return positions, nil
}

// isValidPlaceholder checks if a 2-character string is a valid hashcat placeholder
func isValidPlaceholder(placeholder string) bool {
	if len(placeholder) != 2 || placeholder[0] != '?' {
		return false
	}

	// Valid second characters: l, u, d, s, a, b, h, H, 1-4
	second := placeholder[1]
	switch second {
	case 'l', 'u', 'd', 's', 'a', 'b', 'h', 'H':
		return true
	case '1', '2', '3', '4':
		return true
	default:
		return false
	}
}

// GenerateIncrementLayers generates masks for each length from min to max
// For increment mode: returns shortest to longest
// For increment_inverse mode: returns longest to shortest
func GenerateIncrementLayers(mask string, minLength int, maxLength int, isInverse bool) ([]string, error) {
	if minLength < 1 {
		return nil, fmt.Errorf("min_length must be at least 1")
	}

	if maxLength < minLength {
		return nil, fmt.Errorf("max_length (%d) must be >= min_length (%d)", maxLength, minLength)
	}

	// Parse the mask into positions
	positions, err := ParseMask(mask)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mask: %w", err)
	}

	maskLength := len(positions)

	// Validate that min/max don't exceed mask length
	if minLength > maskLength {
		return nil, fmt.Errorf("min_length (%d) exceeds mask length (%d)", minLength, maskLength)
	}

	// Cap maxLength at mask length
	if maxLength > maskLength {
		maxLength = maskLength
	}

	// Generate layer masks
	var layers []string
	for length := minLength; length <= maxLength; length++ {
		layerMask := buildMaskFromPositions(positions[:length])
		layers = append(layers, layerMask)
	}

	// Reverse for increment_inverse mode (longest first)
	if isInverse {
		for i, j := 0, len(layers)-1; i < j; i, j = i+1, j-1 {
			layers[i], layers[j] = layers[j], layers[i]
		}
	}

	return layers, nil
}

// buildMaskFromPositions reconstructs a mask string from positions
func buildMaskFromPositions(positions []MaskPosition) string {
	var sb strings.Builder
	for _, pos := range positions {
		sb.WriteString(pos.Placeholder)
	}
	return sb.String()
}

// GetMaskLength returns the number of positions in a mask (not the string length)
func GetMaskLength(mask string) (int, error) {
	positions, err := ParseMask(mask)
	if err != nil {
		return 0, err
	}
	return len(positions), nil
}

// CalculateEffectiveKeyspace calculates the total number of candidates for a mask by
// multiplying the charset size for each position, and optionally multiplying by a wordlist
// line count for hybrid attack modes (-a 6, -a 7) where each candidate is wordlist × mask.
//
// customCharsets maps slot keys ("1"-"4") to inline charset definitions (e.g., "?u?d").
// charsetFiles maps slot keys to file-based charset references; CharsetFileRef.ByteCount
// is authoritative for that slot's size and takes priority over an inline definition.
// wordlistLines is the number of words in the wordlist for hybrid modes; pass 0 (or 1) for
// pure mask attacks (-a 3) — values < 2 are treated as "no multiplier" and the function
// returns just mask_candidates.
//
// Examples:
//   - ?l?l, wordlistLines=0  → 26 * 26 = 676
//   - ?1?1 with charset_1=?u?d, wordlistLines=0 → 36 * 36 = 1,296
//   - ?1?1?1 with charset_1 = 256-byte hcchr file, wordlistLines=0 → 256^3 = 16,777,216
//   - ?1 with charset_1 = 256-byte hcchr file, wordlistLines = 23,641,256 (hybrid mode 6)
//     → 256 * 23,641,256 = 6,052,161,536
//
// Returns an error if the mask references an undefined custom-charset slot.
func CalculateEffectiveKeyspace(mask string, customCharsets models.CustomCharsets, charsetFiles models.CustomCharsetFiles, wordlistLines int64) (int64, error) {
	positions, err := ParseMask(mask)
	if err != nil {
		return 0, fmt.Errorf("failed to parse mask: %w", err)
	}

	// Pre-resolve custom charset sizes (ordered 1-4 to allow back-references)
	// File charsets win over inline definitions for the same slot.
	resolvedSizes := make(map[string]int64)
	for _, slot := range []string{"1", "2", "3", "4"} {
		if cf, ok := charsetFiles[slot]; ok && cf.ByteCount > 0 {
			resolvedSizes[slot] = int64(cf.ByteCount)
			continue
		}
		def, ok := customCharsets[slot]
		if !ok || def == "" {
			continue
		}
		size, err := ResolveCharsetSize(def, customCharsets, resolvedSizes)
		if err != nil {
			return 0, fmt.Errorf("failed to resolve custom charset %s (%q): %w", slot, def, err)
		}
		resolvedSizes[slot] = size
	}

	var keyspace int64 = 1
	for _, pos := range positions {
		if pos.IsLiteral {
			// Literal characters don't multiply keyspace (they're fixed)
			continue
		}

		charsetSize, err := getCharsetSize(pos.Placeholder, resolvedSizes)
		if err != nil {
			return 0, err
		}
		keyspace *= charsetSize
	}

	if wordlistLines > 1 {
		keyspace *= wordlistLines
	}

	return keyspace, nil
}

// ResolveCharsetSize calculates the number of unique characters in a charset definition.
// A definition can contain built-in placeholders (?l, ?u, ?d, etc.), references to
// earlier custom charsets (?1-?4), and literal characters.
// The resolved map contains sizes of previously-resolved custom charsets.
func ResolveCharsetSize(definition string, customCharsets map[string]string, resolved map[string]int64) (int64, error) {
	if definition == "" {
		return 0, fmt.Errorf("charset definition cannot be empty")
	}

	var totalSize int64
	uniqueLiterals := make(map[byte]bool)

	i := 0
	for i < len(definition) {
		if definition[i] == '?' && i+1 < len(definition) {
			placeholder := definition[i : i+2]
			second := definition[i+1]

			switch second {
			case 'l':
				totalSize += 26
			case 'u':
				totalSize += 26
			case 'd':
				totalSize += 10
			case 's':
				totalSize += 33
			case 'a':
				totalSize += 95
			case 'b':
				totalSize += 256
			case 'h':
				totalSize += 16
			case 'H':
				totalSize += 16
			case '1', '2', '3', '4':
				slot := string(second)
				if size, ok := resolved[slot]; ok {
					totalSize += size
				} else {
					return 0, fmt.Errorf("custom charset ?%s not defined or forward-referenced in %q", slot, definition)
				}
			default:
				return 0, fmt.Errorf("invalid placeholder %s in charset definition %q", placeholder, definition)
			}
			i += 2
		} else {
			// Literal character
			if !uniqueLiterals[definition[i]] {
				uniqueLiterals[definition[i]] = true
				totalSize++
			}
			i++
		}
	}

	return totalSize, nil
}

// getCharsetSize returns the number of characters in a placeholder's charset.
// Returns an error if the placeholder is a custom-charset slot (?1-?4) that
// has no resolved size — callers must provide either an inline definition or a
// file charset for every slot referenced in the mask.
func getCharsetSize(placeholder string, resolvedCustom map[string]int64) (int64, error) {
	switch placeholder {
	case "?l": // lowercase letters (a-z)
		return 26, nil
	case "?u": // uppercase letters (A-Z)
		return 26, nil
	case "?d": // digits (0-9)
		return 10, nil
	case "?s": // special characters
		return 33, nil
	case "?a": // all printable ASCII
		return 95, nil
	case "?b": // all bytes (0x00-0xff)
		return 256, nil
	case "?h": // lowercase hex (0-9a-f)
		return 16, nil
	case "?H": // uppercase hex (0-9A-F)
		return 16, nil
	default:
		// Custom charsets (?1-?4) - look up resolved size
		if len(placeholder) == 2 && placeholder[0] == '?' {
			slot := string(placeholder[1])
			if size, ok := resolvedCustom[slot]; ok {
				return size, nil
			}
			return 0, fmt.Errorf("mask references undefined custom charset ?%s (no inline definition or file charset provided)", slot)
		}
		return 0, fmt.Errorf("unknown placeholder %q in mask", placeholder)
	}
}
