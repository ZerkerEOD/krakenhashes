package models

import "testing"

// TestIsMutatableAttack covers every attack mode the platform offers. Only attacks whose
// mutation is separable from the wordlist qualify for loopback: straight WITH rules (the
// rules mutate) and the two hybrid modes (the mask mutates).
func TestIsMutatableAttack(t *testing.T) {
	tests := []struct {
		name    string
		mode    AttackMode
		ruleIDs IDArray
		want    bool
	}{
		{
			name:    "Straight with rules - rules are the mutation",
			mode:    AttackModeStraight,
			ruleIDs: IDArray{"1"},
			want:    true,
		},
		{
			name:    "Straight with multiple rules",
			mode:    AttackModeStraight,
			ruleIDs: IDArray{"1", "2"},
			want:    true,
		},
		{
			name:    "Straight with empty rule list - nothing mutates",
			mode:    AttackModeStraight,
			ruleIDs: IDArray{},
			want:    false,
		},
		{
			name:    "Straight with nil rule list - nothing mutates",
			mode:    AttackModeStraight,
			ruleIDs: nil,
			want:    false,
		},
		{
			name:    "Combination - no clean mutation side",
			mode:    AttackModeCombination,
			ruleIDs: nil,
			want:    false,
		},
		{
			name:    "Brute-force - no wordlist to swap the delta into",
			mode:    AttackModeBruteForce,
			ruleIDs: nil,
			want:    false,
		},
		{
			name:    "Hybrid wordlist+mask - mask is the mutation",
			mode:    AttackModeHybridWordlistMask,
			ruleIDs: nil,
			want:    true,
		},
		{
			name:    "Hybrid mask+wordlist - mask is the mutation",
			mode:    AttackModeHybridMaskWordlist,
			ruleIDs: nil,
			want:    true,
		},
		{
			name:    "Association - 1:1 wordlist mapping, no loopback semantics",
			mode:    AttackModeAssociation,
			ruleIDs: nil,
			want:    false,
		},
		{
			name:    "Brute-force ignores stray rules",
			mode:    AttackModeBruteForce,
			ruleIDs: IDArray{"1"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMutatableAttack(tt.mode, tt.ruleIDs); got != tt.want {
				t.Errorf("IsMutatableAttack(%d, %v) = %v, want %v", tt.mode, tt.ruleIDs, got, tt.want)
			}
		})
	}
}
