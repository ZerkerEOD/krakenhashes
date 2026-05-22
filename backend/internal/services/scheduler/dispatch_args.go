package scheduler

import (
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/google/uuid"
)

// Attack-mode constants. Keep these aligned with the hashcat -a values; the
// scheduler treats them as opaque integers for dispatch but the per-mode
// switch needs the symbolic names for readability.
const (
	AttackModeStraight    = 0 // -a 0 dict + rules
	AttackModeCombinator  = 1 // -a 1 two wordlists
	AttackModeMask        = 3 // -a 3 mask brute-force
	AttackModeHybridWLM   = 6 // -a 6 wordlist + mask
	AttackModeHybridMWL   = 7 // -a 7 mask + wordlist
	AttackModeAssociation = 9 // -a 9 1:1 hash:candidate
)

// BuildTaskAssignment constructs the attack-mode-specific portion of a
// TaskAssignmentPayload for one dispatched chunk. The caller fills in
// fields that require external lookups (BinaryPath, HashlistPath,
// HashType, HashlistID, ChunkDuration, ReportInterval, ExtraParameters,
// JobAdditionalArgs, EnabledDevices, Client*, BaseKeyspace) before
// sending to the agent. Keeping those out of this function makes it
// purely unit-testable.
//
// Per `[plan §7]`:
//
//   -a 0  WordlistPaths=[wl], RulePaths=rules[]
//   -a 1  WordlistPaths=[dict1, dict2]
//   -a 3  Mask, no wordlist
//   -a 6  WordlistPaths=[dict], Mask
//   -a 7  WordlistPaths=[dict], Mask
//   -a 9  WordlistPaths=[wl], AssociationWordlistPath=wl, RulePaths=rules[]
//
// IsKeyspaceSplit is set true for every mode because the rewrite uses
// --skip/--limit on every chunk. The agent's hashcat command builder at
// hashcat_executor.go:644-671 reads this flag to emit the flags only
// when set.
func BuildTaskAssignment(
	unit *models.SchedulingUnit,
	taskID uuid.UUID,
	rangeStart, rangeEnd int64,
	effStart, effEnd int64,
) (*wsservice.TaskAssignmentPayload, error) {
	if unit == nil {
		return nil, errors.New("BuildTaskAssignment: unit is nil")
	}
	if rangeEnd <= rangeStart {
		return nil, fmt.Errorf("BuildTaskAssignment: invalid range [%d, %d)", rangeStart, rangeEnd)
	}

	p := &wsservice.TaskAssignmentPayload{
		TaskID:                 taskID.String(),
		JobExecutionID:         unit.ParentJobID.String(),
		AttackMode:             unit.AttackMode,
		KeyspaceStart:          rangeStart,
		KeyspaceEnd:            rangeEnd,
		EffectiveKeyspaceStart: effStart,
		EffectiveKeyspaceEnd:   effEnd,
		IsKeyspaceSplit:        true,
		OutputFormat:           "3", // hash:plain — same as legacy at job_websocket_integration.go:832
	}

	switch unit.AttackMode {

	case AttackModeStraight:
		if len(unit.WordlistRefs) < 1 {
			return nil, errors.New("-a 0 requires at least one wordlist ref")
		}
		p.WordlistPaths = []string{unit.WordlistRefs[0]}
		p.RulePaths = append([]string(nil), unit.RuleFileRefs...)

	case AttackModeCombinator:
		if len(unit.WordlistRefs) != 2 {
			return nil, fmt.Errorf("-a 1 requires exactly two wordlist refs, got %d", len(unit.WordlistRefs))
		}
		if len(unit.RuleFileRefs) > 0 {
			return nil, errors.New("-a 1 does not support -r rules")
		}
		p.WordlistPaths = []string{unit.WordlistRefs[0], unit.WordlistRefs[1]}

	case AttackModeMask:
		if unit.MaskString == nil || *unit.MaskString == "" {
			return nil, errors.New("-a 3 requires a mask")
		}
		if len(unit.WordlistRefs) > 0 {
			return nil, errors.New("-a 3 does not take wordlists")
		}
		if len(unit.RuleFileRefs) > 0 {
			return nil, errors.New("-a 3 does not take -r rules")
		}
		p.Mask = *unit.MaskString

	case AttackModeHybridWLM:
		if len(unit.WordlistRefs) != 1 {
			return nil, fmt.Errorf("-a 6 requires exactly one wordlist ref, got %d", len(unit.WordlistRefs))
		}
		if unit.MaskString == nil || *unit.MaskString == "" {
			return nil, errors.New("-a 6 requires a mask")
		}
		if len(unit.RuleFileRefs) > 0 {
			return nil, errors.New("-a 6 does not support -r rules")
		}
		p.WordlistPaths = []string{unit.WordlistRefs[0]}
		p.Mask = *unit.MaskString

	case AttackModeHybridMWL:
		if len(unit.WordlistRefs) != 1 {
			return nil, fmt.Errorf("-a 7 requires exactly one wordlist ref, got %d", len(unit.WordlistRefs))
		}
		if unit.MaskString == nil || *unit.MaskString == "" {
			return nil, errors.New("-a 7 requires a mask")
		}
		if len(unit.RuleFileRefs) > 0 {
			return nil, errors.New("-a 7 does not support -r rules")
		}
		p.WordlistPaths = []string{unit.WordlistRefs[0]}
		p.Mask = *unit.MaskString

	case AttackModeAssociation:
		// -a 9 takes a single association wordlist that's 1:1 with the
		// hashlist. Rules are allowed and stacked. The legacy
		// integration at job_websocket_integration.go uses both
		// WordlistPaths[0] and AssociationWordlistPath; the agent
		// reads AssociationWordlistPath to know where the candidate
		// list lives.
		if len(unit.WordlistRefs) != 1 {
			return nil, fmt.Errorf("-a 9 requires exactly one association wordlist ref, got %d", len(unit.WordlistRefs))
		}
		p.WordlistPaths = []string{unit.WordlistRefs[0]}
		p.AssociationWordlistPath = unit.WordlistRefs[0]
		p.RulePaths = append([]string(nil), unit.RuleFileRefs...)

	default:
		return nil, fmt.Errorf("BuildTaskAssignment: unsupported attack_mode %d", unit.AttackMode)
	}

	// Custom charsets are mode-agnostic — mask attacks may reference
	// them; non-mask modes simply ignore. Forward whatever the unit
	// has.
	if len(unit.CustomCharsets) > 0 {
		// CustomCharsets in the unit is stored as JSONB; convert to
		// the wire-format map. The conversion is a typed unmarshal at
		// the caller side because SchedulingUnit holds it as
		// json.RawMessage. For B.1, the parsed structure isn't needed
		// here — that's the cycle's job, which has access to typed
		// PresetJob fields.
		// (Intentional no-op in this function; cycle.go fills
		// CustomCharsets / CharsetFiles / HexCharset.)
	}

	return p, nil
}
