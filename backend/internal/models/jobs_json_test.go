package models

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// assertKeysPresent marshals v and fails the test for every key that is missing
// from the resulting JSON object.
func assertKeysPresent(t *testing.T, v interface{}, keys ...string) {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			t.Errorf("key %q missing from JSON; omitempty dropped a meaningful zero value. Got: %s", key, raw)
		}
	}
}

// TestJobWorkflowStepJSON_ZeroValuesPresent locks the GH #78 regression: attack mode 0
// (AttackModeStraight) and priority 0 are meaningful values, but `omitempty` erased them
// from the wire. Straight+rules is the primary loopback-eligible attack, so the workflow
// editor — which keys per-step loopback eligibility off preset_job_attack_mode — rendered
// the Loopback checkbox permanently disabled after a save/reload.
func TestJobWorkflowStepJSON_ZeroValuesPresent(t *testing.T) {
	step := JobWorkflowStep{
		ID:                  1,
		JobWorkflowID:       uuid.New(),
		PresetJobID:         uuid.New(),
		StepOrder:           1,
		LoopbackEnabled:     false,
		PresetJobName:       "Straight + best64",
		PresetJobAttackMode: AttackModeStraight,
		PresetJobPriority:   0,
	}

	assertKeysPresent(t, step,
		"preset_job_attack_mode",
		"preset_job_priority",
		"loopback_enabled",
	)
}

// TestPresetJobBasicJSON_AttackModeZeroPresent locks the create-page contract. PresetJobBasic
// already emitted attack_mode 0 correctly, but only because it happens to carry no omitempty;
// this pins that so the JobWorkflowStep bug cannot be reintroduced here by "consistency".
func TestPresetJobBasicJSON_AttackModeZeroPresent(t *testing.T) {
	basic := PresetJobBasic{
		ID:         uuid.New(),
		Name:       "Straight + best64",
		AttackMode: AttackModeStraight,
	}

	assertKeysPresent(t, basic, "attack_mode", "allow_high_priority_override")
}
