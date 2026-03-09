package utils

import (
	"testing"
)

func TestParseMask(t *testing.T) {
	tests := []struct {
		name      string
		mask      string
		wantLen   int
		wantErr   bool
	}{
		{
			name:    "simple lowercase mask",
			mask:    "?l?l?l",
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "mixed placeholders",
			mask:    "?l?d?u?s",
			wantLen: 4,
			wantErr: false,
		},
		{
			name:    "custom charset",
			mask:    "?1?1?2",
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "with literal characters",
			mask:    "pass?l?d",
			wantLen: 6,
			wantErr: false,
		},
		{
			name:    "empty mask",
			mask:    "",
			wantLen: 0,
			wantErr: true,
		},
		{
			name:    "incomplete placeholder",
			mask:    "?l?",
			wantLen: 0,
			wantErr: true,
		},
		{
			name:    "invalid placeholder",
			mask:    "?x",
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			positions, err := ParseMask(tt.mask)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMask() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(positions) != tt.wantLen {
				t.Errorf("ParseMask() got %d positions, want %d", len(positions), tt.wantLen)
			}
		})
	}
}

func TestGenerateIncrementLayers(t *testing.T) {
	tests := []struct {
		name      string
		mask      string
		minLength int
		maxLength int
		isInverse bool
		want      []string
		wantErr   bool
	}{
		{
			name:      "simple increment",
			mask:      "?l?l?l",
			minLength: 2,
			maxLength: 3,
			isInverse: false,
			want:      []string{"?l?l", "?l?l?l"},
			wantErr:   false,
		},
		{
			name:      "simple increment inverse",
			mask:      "?l?l?l",
			minLength: 2,
			maxLength: 3,
			isInverse: true,
			want:      []string{"?l?l?l", "?l?l"},
			wantErr:   false,
		},
		{
			name:      "mixed placeholders",
			mask:      "?l?d?u?s",
			minLength: 2,
			maxLength: 4,
			isInverse: false,
			want:      []string{"?l?d", "?l?d?u", "?l?d?u?s"},
			wantErr:   false,
		},
		{
			name:      "single length",
			mask:      "?l?l?l",
			minLength: 2,
			maxLength: 2,
			isInverse: false,
			want:      []string{"?l?l"},
			wantErr:   false,
		},
		{
			name:      "min > mask length",
			mask:      "?l?l",
			minLength: 5,
			maxLength: 6,
			isInverse: false,
			want:      nil,
			wantErr:   true,
		},
		{
			name:      "max > mask length (should cap)",
			mask:      "?l?l?l",
			minLength: 2,
			maxLength: 10,
			isInverse: false,
			want:      []string{"?l?l", "?l?l?l"},
			wantErr:   false,
		},
		{
			name:      "min < 1",
			mask:      "?l?l?l",
			minLength: 0,
			maxLength: 3,
			isInverse: false,
			want:      nil,
			wantErr:   true,
		},
		{
			name:      "max < min",
			mask:      "?l?l?l",
			minLength: 3,
			maxLength: 2,
			isInverse: false,
			want:      nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateIncrementLayers(tt.mask, tt.minLength, tt.maxLength, tt.isInverse)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateIncrementLayers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("GenerateIncrementLayers() got %d layers, want %d", len(got), len(tt.want))
					return
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("GenerateIncrementLayers() layer %d = %v, want %v", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestGetMaskLength(t *testing.T) {
	tests := []struct {
		name    string
		mask    string
		want    int
		wantErr bool
	}{
		{
			name:    "simple mask",
			mask:    "?l?l?l",
			want:    3,
			wantErr: false,
		},
		{
			name:    "mixed mask",
			mask:    "?l?d?u?s",
			want:    4,
			wantErr: false,
		},
		{
			name:    "with literals",
			mask:    "pass?l?d",
			want:    6,
			wantErr: false,
		},
		{
			name:    "empty mask",
			mask:    "",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetMaskLength(tt.mask)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetMaskLength() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("GetMaskLength() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateEffectiveKeyspace(t *testing.T) {
	tests := []struct {
		name           string
		mask           string
		customCharsets map[string]string
		want           int64
		wantErr        bool
	}{
		{
			name:           "simple lowercase",
			mask:           "?l?l",
			customCharsets: nil,
			want:           26 * 26,
		},
		{
			name:           "mixed builtin",
			mask:           "?l?d",
			customCharsets: nil,
			want:           26 * 10,
		},
		{
			name:           "all printable",
			mask:           "?a?a?a",
			customCharsets: nil,
			want:           95 * 95 * 95,
		},
		{
			name:           "with literal (does not multiply)",
			mask:           "pass?l?d",
			customCharsets: nil,
			want:           26 * 10,
		},
		{
			name:           "custom charset ?u?d = 36",
			mask:           "?1?1?1?1",
			customCharsets: map[string]string{"1": "?u?d"},
			want:           36 * 36 * 36 * 36, // 1,679,616
		},
		{
			name:           "HP iLO use case: ?u?d 8 chars",
			mask:           "?1?1?1?1?1?1?1?1",
			customCharsets: map[string]string{"1": "?u?d"},
			want:           2821109907456, // 36^8
		},
		{
			name:           "two custom charsets",
			mask:           "?1?1?2?2",
			customCharsets: map[string]string{"1": "?u?d", "2": "?s?l"},
			want:           36 * 36 * 59 * 59, // ?u?d=36, ?s?l=33+26=59
		},
		{
			name:           "charset with literal chars",
			mask:           "?1?1",
			customCharsets: map[string]string{"1": "abc"},
			want:           3 * 3,
		},
		{
			name:           "charset with mixed literals and placeholder",
			mask:           "?1?1",
			customCharsets: map[string]string{"1": "?dABC"},
			want:           14 * 14, // 10 digits + 4 literals
		},
		{
			name:           "charset referencing earlier charset",
			mask:           "?2?2",
			customCharsets: map[string]string{"1": "?u?d", "2": "?1?s"},
			want:           69 * 69, // charset 2 = charset 1 (36) + ?s (33) = 69
		},
		{
			name:           "nil charsets with no custom refs",
			mask:           "?l?u?d",
			customCharsets: nil,
			want:           26 * 26 * 10,
		},
		{
			name:           "empty charsets map",
			mask:           "?l?u",
			customCharsets: map[string]string{},
			want:           26 * 26,
		},
		{
			name:           "undefined custom charset falls back to 26",
			mask:           "?1?1",
			customCharsets: nil,
			want:           26 * 26, // falls back to 26 for undefined charsets
		},
		{
			name:           "forward reference errors",
			mask:           "?1?1",
			customCharsets: map[string]string{"1": "?2"},
			want:           0,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateEffectiveKeyspace(tt.mask, tt.customCharsets)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateEffectiveKeyspace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("CalculateEffectiveKeyspace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveCharsetSize(t *testing.T) {
	tests := []struct {
		name           string
		definition     string
		customCharsets map[string]string
		resolved       map[string]int64
		want           int64
		wantErr        bool
	}{
		{
			name:       "builtin lowercase",
			definition: "?l",
			resolved:   map[string]int64{},
			want:       26,
		},
		{
			name:       "builtin union",
			definition: "?u?d",
			resolved:   map[string]int64{},
			want:       36,
		},
		{
			name:       "literal chars",
			definition: "abcdef",
			resolved:   map[string]int64{},
			want:       6,
		},
		{
			name:       "duplicate literals counted once",
			definition: "aabbcc",
			resolved:   map[string]int64{},
			want:       3,
		},
		{
			name:       "mixed literal and placeholder",
			definition: "?dABCD",
			resolved:   map[string]int64{},
			want:       14, // 10 + 4
		},
		{
			name:       "reference to resolved charset",
			definition: "?1",
			resolved:   map[string]int64{"1": 36},
			want:       36,
		},
		{
			name:       "all builtins",
			definition: "?a",
			resolved:   map[string]int64{},
			want:       95,
		},
		{
			name:       "hex lowercase",
			definition: "?h",
			resolved:   map[string]int64{},
			want:       16,
		},
		{
			name:       "empty definition",
			definition: "",
			resolved:   map[string]int64{},
			want:       0,
			wantErr:    true,
		},
		{
			name:       "unresolved reference errors",
			definition: "?3",
			resolved:   map[string]int64{},
			want:       0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveCharsetSize(tt.definition, tt.customCharsets, tt.resolved)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveCharsetSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ResolveCharsetSize() = %v, want %v", got, tt.want)
			}
		})
	}
}
