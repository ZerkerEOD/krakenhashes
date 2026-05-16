package scheduler

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

func ptr(s string) *string { return &s }

func newUnit(mode int, wordlists []string, rules []string, mask *string) *models.SchedulingUnit {
	return &models.SchedulingUnit{
		ID:           uuid.New(),
		ParentJobID:  uuid.New(),
		AttackMode:   mode,
		WordlistRefs: wordlists,
		RuleFileRefs: rules,
		MaskString:   mask,
	}
}

func TestBuildTaskAssignment_NilUnit(t *testing.T) {
	if _, err := BuildTaskAssignment(nil, uuid.New(), 0, 100); err == nil {
		t.Fatal("expected error on nil unit")
	}
}

func TestBuildTaskAssignment_BadRange(t *testing.T) {
	u := newUnit(AttackModeStraight, []string{"wl"}, nil, nil)
	if _, err := BuildTaskAssignment(u, uuid.New(), 100, 50); err == nil {
		t.Fatal("expected error when rangeEnd <= rangeStart")
	}
	if _, err := BuildTaskAssignment(u, uuid.New(), 100, 100); err == nil {
		t.Fatal("expected error when rangeEnd == rangeStart")
	}
}

// -a 0 -------------------------------------------------------------------

func TestBuildTaskAssignment_A0_StraightWithRules(t *testing.T) {
	u := newUnit(AttackModeStraight, []string{"wordlists/rockyou.txt"}, []string{"rules/best64.rule", "rules/d3ad0ne.rule"}, nil)
	taskID := uuid.New()

	p, err := BuildTaskAssignment(u, taskID, 1000, 1500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.TaskID != taskID.String() {
		t.Errorf("TaskID = %q, want %q", p.TaskID, taskID.String())
	}
	if p.AttackMode != AttackModeStraight {
		t.Errorf("AttackMode = %d, want 0", p.AttackMode)
	}
	if p.KeyspaceStart != 1000 || p.KeyspaceEnd != 1500 {
		t.Errorf("range = [%d,%d), want [1000,1500)", p.KeyspaceStart, p.KeyspaceEnd)
	}
	if !p.IsKeyspaceSplit {
		t.Error("IsKeyspaceSplit must be true for -a 0")
	}
	if len(p.WordlistPaths) != 1 || p.WordlistPaths[0] != "wordlists/rockyou.txt" {
		t.Errorf("WordlistPaths = %v", p.WordlistPaths)
	}
	if len(p.RulePaths) != 2 || p.RulePaths[0] != "rules/best64.rule" || p.RulePaths[1] != "rules/d3ad0ne.rule" {
		t.Errorf("RulePaths = %v, want stacked rules in order", p.RulePaths)
	}
	if p.Mask != "" {
		t.Errorf("Mask should be empty for -a 0, got %q", p.Mask)
	}
	if p.AssociationWordlistPath != "" {
		t.Errorf("AssociationWordlistPath should be empty for -a 0, got %q", p.AssociationWordlistPath)
	}
}

func TestBuildTaskAssignment_A0_NoWordlistFails(t *testing.T) {
	u := newUnit(AttackModeStraight, nil, []string{"rules/best64.rule"}, nil)
	if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 0 with no wordlist should fail")
	}
}

func TestBuildTaskAssignment_A0_NoRulesIsValid(t *testing.T) {
	u := newUnit(AttackModeStraight, []string{"wl"}, nil, nil)
	p, err := BuildTaskAssignment(u, uuid.New(), 0, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.RulePaths) != 0 {
		t.Errorf("expected empty RulePaths, got %v", p.RulePaths)
	}
}

// -a 1 -------------------------------------------------------------------

func TestBuildTaskAssignment_A1_Combinator(t *testing.T) {
	u := newUnit(AttackModeCombinator, []string{"wl/left.txt", "wl/right.txt"}, nil, nil)
	p, err := BuildTaskAssignment(u, uuid.New(), 0, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.WordlistPaths) != 2 || p.WordlistPaths[0] != "wl/left.txt" || p.WordlistPaths[1] != "wl/right.txt" {
		t.Errorf("WordlistPaths = %v, want [left, right] in order", p.WordlistPaths)
	}
}

func TestBuildTaskAssignment_A1_WrongCountFails(t *testing.T) {
	for _, count := range []int{0, 1, 3} {
		wls := make([]string, count)
		for i := range wls {
			wls[i] = "wl"
		}
		u := newUnit(AttackModeCombinator, wls, nil, nil)
		if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
			t.Errorf("-a 1 with %d wordlists should fail", count)
		}
	}
}

func TestBuildTaskAssignment_A1_RejectsRules(t *testing.T) {
	u := newUnit(AttackModeCombinator, []string{"a", "b"}, []string{"r.rule"}, nil)
	if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 1 with rules should fail (hashcat doesn't accept -r in combinator)")
	}
}

// -a 3 -------------------------------------------------------------------

func TestBuildTaskAssignment_A3_Mask(t *testing.T) {
	u := newUnit(AttackModeMask, nil, nil, ptr("?a?a?a?a"))
	p, err := BuildTaskAssignment(u, uuid.New(), 0, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mask != "?a?a?a?a" {
		t.Errorf("Mask = %q, want ?a?a?a?a", p.Mask)
	}
	if len(p.WordlistPaths) != 0 {
		t.Errorf("WordlistPaths should be empty for -a 3, got %v", p.WordlistPaths)
	}
}

func TestBuildTaskAssignment_A3_NoMaskFails(t *testing.T) {
	u := newUnit(AttackModeMask, nil, nil, nil)
	if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 3 without mask should fail")
	}

	empty := ""
	u2 := newUnit(AttackModeMask, nil, nil, &empty)
	if _, err := BuildTaskAssignment(u2, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 3 with empty-string mask should fail")
	}
}

func TestBuildTaskAssignment_A3_RejectsWordlist(t *testing.T) {
	u := newUnit(AttackModeMask, []string{"wl"}, nil, ptr("?d?d?d"))
	if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 3 with a wordlist should fail")
	}
}

// -a 6 -------------------------------------------------------------------

func TestBuildTaskAssignment_A6_HybridWordlistMask(t *testing.T) {
	u := newUnit(AttackModeHybridWLM, []string{"wl/rockyou.txt"}, nil, ptr("?d?d?d?d"))
	p, err := BuildTaskAssignment(u, uuid.New(), 0, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.WordlistPaths) != 1 || p.WordlistPaths[0] != "wl/rockyou.txt" {
		t.Errorf("WordlistPaths = %v", p.WordlistPaths)
	}
	if p.Mask != "?d?d?d?d" {
		t.Errorf("Mask = %q", p.Mask)
	}
}

func TestBuildTaskAssignment_A6_NeedsBoth(t *testing.T) {
	u := newUnit(AttackModeHybridWLM, []string{"wl"}, nil, nil)
	if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 6 without mask should fail")
	}

	u2 := newUnit(AttackModeHybridWLM, nil, nil, ptr("?d?d"))
	if _, err := BuildTaskAssignment(u2, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 6 without wordlist should fail")
	}
}

// -a 7 -------------------------------------------------------------------

func TestBuildTaskAssignment_A7_HybridMaskWordlist(t *testing.T) {
	u := newUnit(AttackModeHybridMWL, []string{"wl/rockyou.txt"}, nil, ptr("?d?d?d?d"))
	p, err := BuildTaskAssignment(u, uuid.New(), 0, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.WordlistPaths) != 1 || p.WordlistPaths[0] != "wl/rockyou.txt" {
		t.Errorf("WordlistPaths = %v", p.WordlistPaths)
	}
	if p.Mask != "?d?d?d?d" {
		t.Errorf("Mask = %q", p.Mask)
	}
}

// -a 9 -------------------------------------------------------------------

func TestBuildTaskAssignment_A9_Association(t *testing.T) {
	u := newUnit(AttackModeAssociation, []string{"wl/usernames.txt"}, []string{"rules/best64.rule"}, nil)
	p, err := BuildTaskAssignment(u, uuid.New(), 100, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.WordlistPaths) != 1 || p.WordlistPaths[0] != "wl/usernames.txt" {
		t.Errorf("WordlistPaths = %v", p.WordlistPaths)
	}
	if p.AssociationWordlistPath != "wl/usernames.txt" {
		t.Errorf("AssociationWordlistPath = %q, want same as WordlistPaths[0]", p.AssociationWordlistPath)
	}
	if len(p.RulePaths) != 1 || p.RulePaths[0] != "rules/best64.rule" {
		t.Errorf("RulePaths = %v", p.RulePaths)
	}
}

func TestBuildTaskAssignment_A9_NoWordlistFails(t *testing.T) {
	u := newUnit(AttackModeAssociation, nil, nil, nil)
	if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
		t.Fatal("-a 9 with no association wordlist should fail")
	}
}

// Unsupported mode -------------------------------------------------------

func TestBuildTaskAssignment_UnsupportedMode(t *testing.T) {
	u := newUnit(8, nil, nil, nil) // 8 = PRINCE, removed from hashcat
	if _, err := BuildTaskAssignment(u, uuid.New(), 0, 100); err == nil {
		t.Fatal("expected error for unsupported attack mode")
	}
}

// IsKeyspaceSplit must be true for every supported mode -----------------

func TestBuildTaskAssignment_IsKeyspaceSplitAlwaysTrue(t *testing.T) {
	cases := []struct {
		name string
		u    *models.SchedulingUnit
	}{
		{"a0", newUnit(AttackModeStraight, []string{"wl"}, nil, nil)},
		{"a1", newUnit(AttackModeCombinator, []string{"a", "b"}, nil, nil)},
		{"a3", newUnit(AttackModeMask, nil, nil, ptr("?d?d"))},
		{"a6", newUnit(AttackModeHybridWLM, []string{"wl"}, nil, ptr("?d"))},
		{"a7", newUnit(AttackModeHybridMWL, []string{"wl"}, nil, ptr("?d"))},
		{"a9", newUnit(AttackModeAssociation, []string{"wl"}, nil, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := BuildTaskAssignment(tc.u, uuid.New(), 0, 100)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !p.IsKeyspaceSplit {
				t.Error("IsKeyspaceSplit must be true (rewrite uses --skip/--limit on every chunk)")
			}
		})
	}
}
