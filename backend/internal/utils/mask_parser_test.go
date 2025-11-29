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
