package scheduler

import (
	"math/big"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

/*
 * Endgame tapering.
 *
 * PROBLEM
 *
 * Near the end of a job, chunks are still sized by TIME, so every agent takes
 * roughly the same wall-clock bite regardless of speed. The fast agent
 * finishes its share and finds the gap exhausted; the slow agent is still
 * grinding a chunk it started ten minutes ago. The job is not done, the fast
 * agent has nothing to do, and if that fast agent is a rented GPU it bills the
 * entire time it waits.
 *
 * Making cloud chunks BIGGER makes this worse, not better: the cause is the
 * on-prem agent's long chunk, which is why the fix has to apply fleet-wide
 * rather than only to rented agents.
 *
 * FIX
 *
 * Once a unit's remaining undispatched work drops below
 * endgame_threshold_multiple x the chunk duration, stop sizing by time and
 * split what is left in proportion to each agent's speed:
 *
 *     share_i = remaining_base x speed_i / sum(speed_j)
 *
 * Every agent then finishes at approximately the same moment. On-prem-only
 * deployments get shorter job tails; deployments with rented agents also stop
 * paying for the idle wait.
 *
 * sum(speed) must include agents ALREADY RUNNING a chunk on the unit, not just
 * the ones being allocated this cycle. Counting only the idle ones would
 * over-allocate to whoever happened to be free and hand them work a busy
 * faster agent will reach first.
 */

// EndgameInput describes one unit's endgame state.
type EndgameInput struct {
	UnitID uuid.UUID
	// RemainingBase is undispatched base keyspace on this unit.
	RemainingBase int64
	// Participants is every agent that will work this unit: those being
	// allocated now plus those already running a chunk on it.
	Participants []EndgameParticipant
}

// EndgameParticipant is one agent's contribution to a unit.
type EndgameParticipant struct {
	AgentID int
	// Speed is the agent's benchmark speed in effective hashes/sec.
	Speed int64
	// Allocating is true when this agent is receiving a chunk this cycle.
	// Agents already running a chunk still count toward the speed total but
	// receive no share now.
	Allocating bool
}

/*
 * ComputeEndgameShares splits a unit's remaining work proportionally to speed.
 *
 * Returns nil when tapering does not apply, so the caller keeps normal
 * time-boxed sizing. Shares are returned only for allocating agents, but the
 * denominator includes every participant.
 */
func ComputeEndgameShares(in EndgameInput, minChunkBase int64) map[int]int64 {
	if in.RemainingBase <= 0 || len(in.Participants) == 0 {
		return nil
	}

	var totalSpeed int64
	allocating := 0
	for _, p := range in.Participants {
		speed := p.Speed
		if speed <= 0 {
			speed = ConservativeAgentSpeed
		}
		totalSpeed += speed
		if p.Allocating {
			allocating++
		}
	}
	if totalSpeed <= 0 || allocating == 0 {
		return nil
	}

	shares := make(map[int]int64, allocating)
	for _, p := range in.Participants {
		if !p.Allocating {
			continue
		}
		speed := p.Speed
		if speed <= 0 {
			speed = ConservativeAgentSpeed
		}
		// big.Int: remaining x speed overflows int64 for a large wordlist on a
		// fast hash, and truncating the ratio first would systematically
		// under-allocate the fastest agent — the opposite of the goal.
		share := new(big.Int).Mul(big.NewInt(in.RemainingBase), big.NewInt(speed))
		share.Div(share, big.NewInt(totalSpeed))

		var v int64
		if share.IsInt64() {
			v = share.Int64()
		} else {
			v = in.RemainingBase
		}
		if v < minChunkBase {
			v = minChunkBase
		}
		if v > in.RemainingBase {
			v = in.RemainingBase
		}
		shares[p.AgentID] = v
	}
	return shares
}

/*
 * IsEndgame reports whether a unit has entered the endgame.
 *
 * The comparison is in BASE units on both sides. Projecting in effective
 * keyspace instead is the classic error this codebase already documents in
 * sizeChunk: for salted hash types effective_keyspace shrinks as salts crack,
 * so an effective-based threshold would drift as the job progressed.
 */
func IsEndgame(remainingBase int64, chunkDurationSec int, thresholdMultiple int, baseKeyspace int64, effectiveKeyspace *big.Int, aggregateSpeed int64) bool {
	if remainingBase <= 0 || thresholdMultiple <= 0 || aggregateSpeed <= 0 {
		return false
	}
	// How much base keyspace the whole fleet chews through in one
	// threshold-sized window, using the same conversion sizeChunk uses.
	window := sizeChunk(remainingBase, baseKeyspace, effectiveKeyspace, aggregateSpeed,
		chunkDurationSec*thresholdMultiple, 1)
	return remainingBase <= window
}

// UnitRemainingBase reports undispatched base keyspace for a unit, given its
// coverage so far.
func UnitRemainingBase(unit *models.SchedulingUnit, coveredBase int64) int64 {
	if unit == nil || unit.BaseKeyspace == nil {
		return 0
	}
	remaining := *unit.BaseKeyspace - coveredBase
	if remaining < 0 {
		return 0
	}
	return remaining
}
