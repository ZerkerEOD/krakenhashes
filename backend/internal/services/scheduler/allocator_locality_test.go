package scheduler

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// WS7 locality-aware scheduling: these are PURE allocator tests (no DB). They
// pin the contract that localityScore is a TIEBREAK — it prefers a
// file-holding (or network_direct) agent among otherwise-equal candidates, but
// never changes how MANY agents a unit gets, never starves a unit when the
// holder is unavailable, and reduces exactly to the pre-WS7 order when no agent
// holds anything.

func heldSet(files ...string) map[string]bool {
	m := make(map[string]bool, len(files))
	for _, f := range files {
		m[f] = true
	}
	return m
}

// plainAgent is a full_cache agent holding the given wire-path files (none by
// default). Built on agentN so it carries a compatible binary version.
func plainAgent(id int, held ...string) AgentInfo {
	a := agentN(id)
	a.StorageTier = models.StorageTierFullCache
	a.HeldFiles = heldSet(held...)
	return a
}

// networkDirectAgent reads every shareable file off its mount and downloads
// nothing, so it is the strongest locality match regardless of HeldFiles.
func networkDirectAgent(id int) AgentInfo {
	a := agentN(id)
	a.StorageTier = models.StorageTierNetworkDirect
	return a
}

// unitWithFiles is unit() plus a RequiredFiles list (already client/association
// filtered, as buildUnitInfos would leave it).
func unitWithFiles(id uuid.UUID, priority, maxAgents int, createdAtNanos int64, files ...string) UnitInfo {
	u := unit(id, priority, maxAgents, 0, createdAtNanos)
	u.RequiredFiles = files
	return u
}

// (a) Two compatible idle agents; one already holds the unit's file. With a
// single slot the holder must be chosen even though the non-holder is earlier
// in the pool.
func TestAllocator_Locality_PrefersHolder(t *testing.T) {
	u := unitWithFiles(uuid.New(), 5, 1, 100, "wordlists/general/rockyou.txt")
	holder := plainAgent(1, "wordlists/general/rockyou.txt")
	plain := plainAgent(2) // holds nothing

	// plain is deliberately listed first: locality must override pool order.
	out := AllocateAgentsByPriority([]UnitInfo{u}, []AgentInfo{plain, holder}, OverflowEnforceMaxAgents, alwaysCompatible)

	if len(out) != 1 {
		t.Fatalf("capacity 1 should yield exactly one allocation, got %d", len(out))
	}
	if out[0].AgentID != holder.ID {
		t.Fatalf("expected the file-holder (agent %d) to be chosen, got agent %d", holder.ID, out[0].AgentID)
	}
}

// (b) The holder is claimed by a higher-priority unit; the lower-priority unit
// requiring the same file must still be served by the non-holder — locality
// must never starve a unit or reduce total allocations.
func TestAllocator_Locality_DoesNotStarveWhenHolderTakenByHigherPriority(t *testing.T) {
	file := "wordlists/general/rockyou.txt"
	hi := unitWithFiles(uuid.New(), 10, 1, 100, file)
	lo := unitWithFiles(uuid.New(), 5, 1, 200, file)
	holder := plainAgent(1, file)
	plain := plainAgent(2)

	out := AllocateAgentsByPriority([]UnitInfo{lo, hi}, []AgentInfo{holder, plain}, OverflowEnforceMaxAgents, alwaysCompatible)
	set := allocationSet(out)

	if got := set[hi.ID]; len(got) != 1 || got[0] != holder.ID {
		t.Fatalf("higher-priority unit should take the holder; got %v", got)
	}
	if got := set[lo.ID]; len(got) != 1 || got[0] != plain.ID {
		t.Fatalf("lower-priority unit must still be served by the non-holder (no starvation); got %v", got)
	}
	if len(out) != 2 {
		t.Fatalf("both units should be served, got %d allocations", len(out))
	}
}

// (c) A network_direct agent (downloads nothing) is preferred for an on-share
// job over a plain agent that holds only some of the required files.
func TestAllocator_Locality_NetworkDirectPreferred(t *testing.T) {
	f1 := "wordlists/general/a.txt"
	f2 := "rules/hashcat/best64.rule"
	u := unitWithFiles(uuid.New(), 5, 1, 100, f1, f2)

	nd := networkDirectAgent(1)  // score 2 (reads both off the share)
	partial := plainAgent(2, f1) // score 1 (holds only f1)

	// partial is listed first; network_direct's full-coverage score must win.
	out := AllocateAgentsByPriority([]UnitInfo{u}, []AgentInfo{partial, nd}, OverflowEnforceMaxAgents, alwaysCompatible)

	if len(out) != 1 || out[0].AgentID != nd.ID {
		t.Fatalf("network_direct agent should win the on-share job; got %v", out)
	}
}

// (d) No agent holds anything and none is network_direct → every score is 0,
// so allocation is order-preserving: the earliest agent wins, identical to
// pre-WS7 behavior.
func TestAllocator_Locality_NoHoldersPreservesOrder(t *testing.T) {
	file := "wordlists/general/rockyou.txt"
	u := unitWithFiles(uuid.New(), 5, 1, 100, file)
	a1 := plainAgent(1)
	a2 := plainAgent(2)

	out := AllocateAgentsByPriority([]UnitInfo{u}, []AgentInfo{a1, a2}, OverflowEnforceMaxAgents, alwaysCompatible)

	if len(out) != 1 || out[0].AgentID != a1.ID {
		t.Fatalf("with no locality signal the earliest agent must win (FIFO preserved); got %v", out)
	}
}

// Invariant: locality is a tiebreak, not a gate — with unlimited capacity both
// agents are still allocated regardless of who holds the file.
func TestAllocator_Locality_NeverReducesAllocations(t *testing.T) {
	file := "wordlists/general/rockyou.txt"
	u := unitWithFiles(uuid.New(), 5, 0, 100, file) // maxAgents 0 = unlimited
	holder := plainAgent(1, file)
	plain := plainAgent(2)

	out := AllocateAgentsByPriority([]UnitInfo{u}, []AgentInfo{holder, plain}, OverflowEnforceMaxAgents, alwaysCompatible)

	if len(out) != 2 {
		t.Fatalf("locality must not drop allocations: both agents should be allocated, got %d", len(out))
	}
}

// localityScore's own contract, exercised directly.
func TestLocalityScore(t *testing.T) {
	f1, f2 := "wordlists/general/a.txt", "rules/hashcat/best64.rule"
	u := unitWithFiles(uuid.New(), 5, 1, 100, f1, f2)

	if s := localityScore(networkDirectAgent(1), u); s != 2 {
		t.Errorf("network_direct should score the full RequiredFiles count (2), got %d", s)
	}
	if s := localityScore(plainAgent(2, f1), u); s != 1 {
		t.Errorf("agent holding one of two files should score 1, got %d", s)
	}
	if s := localityScore(plainAgent(3, f1, f2), u); s != 2 {
		t.Errorf("agent holding both files should score 2, got %d", s)
	}
	if s := localityScore(plainAgent(4), u); s != 0 {
		t.Errorf("agent holding nothing should score 0, got %d", s)
	}
	// A unit with no shareable files never expresses a preference.
	empty := unit(uuid.New(), 5, 1, 0, 100)
	if s := localityScore(networkDirectAgent(5), empty); s != 0 {
		t.Errorf("unit with no RequiredFiles should score 0 even for network_direct, got %d", s)
	}
}
