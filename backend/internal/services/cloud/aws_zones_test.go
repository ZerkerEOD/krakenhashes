package cloud

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

/*
 * These tests exist because the failure they guard against is invisible.
 *
 * A single-subnet AWS config searches N instance types in ONE availability
 * zone, which on spot is one capacity pool wearing N hats. Every candidate the
 * launch retry loop walks resolves to the same physical inventory, so a refusal
 * on the first predicts the rest — and the operator sees N tidy "trying the
 * next candidate" log lines that look like diligent fallback and are not.
 * Measured: fifteen consecutive refusals against one pool, then a success two
 * cycles after widening to three types across two zones.
 *
 * Nothing here talks to AWS. Every function under test is pure given settings.
 */

func awsWithZones(zones []AWSZone, legacySubnet string, rates map[string]int) *AWSProvider {
	return &AWSProvider{settings: AWSSettings{
		Region:            "us-east-2",
		SubnetID:          legacySubnet,
		Zones:             zones,
		InstanceTypeRates: rates,
	}}
}

var threeTypes = map[string]int{"g4dn.xlarge": 53, "g5.xlarge": 101, "g6.xlarge": 121}

func TestPlacementsFallBackToTheLegacySubnet(t *testing.T) {
	p := awsWithZones(nil, "subnet-legacy", threeTypes).placements()
	if len(p) != 1 {
		t.Fatalf("expected exactly one placement for a legacy config, got %d", len(p))
	}
	if p[0].subnetID != "subnet-legacy" {
		t.Errorf("legacy subnet_id was dropped: got %q, want %q.\n"+
			"Every pre-existing AWS provider config in the field has ONLY subnet_id. "+
			"Dropping it here does not fail loudly — RunInstances just picks a "+
			"default-VPC subnet, so instances quietly launch in the wrong network "+
			"and cannot reach the backend.", p[0].subnetID, "subnet-legacy")
	}
}

func TestPlacementsNeverReturnsEmpty(t *testing.T) {
	// Nothing configured at all: still one placement, with no subnet, which is
	// exactly the pre-zones behaviour of letting EC2 choose.
	p := awsWithZones(nil, "", threeTypes).placements()
	if len(p) != 1 || p[0].subnetID != "" {
		t.Fatalf("expected one placement with no subnet, got %+v", p)
	}
}

func TestZoneWithoutASubnetIsNotALaunchTarget(t *testing.T) {
	p := awsWithZones([]AWSZone{
		{Zone: "us-east-2a", ZoneID: "use2-az1", SubnetID: "subnet-a"},
		{Zone: "us-east-2c", ZoneID: "use2-az3"}, // operator ticked it, never gave it a subnet
	}, "", threeTypes).placements()

	if len(p) != 1 {
		t.Fatalf("expected the subnet-less zone to be skipped, got %d placements: %+v", len(p), p)
	}
	if p[0].zone != "us-east-2a" {
		t.Errorf("wrong zone survived: %q.\n"+
			"A zone with no subnet cannot be launched into. Including it would send "+
			"RunInstances with no SubnetId, which does not fail — it lands in a "+
			"default-VPC subnet with the wrong security groups.", p[0].zone)
	}
}

func TestSearchOffersCrossesZonesWithTypes(t *testing.T) {
	a := awsWithZones([]AWSZone{
		{Zone: "us-east-2a", ZoneID: "use2-az1", SubnetID: "subnet-a"},
		{Zone: "us-east-2b", ZoneID: "use2-az2", SubnetID: "subnet-b"},
	}, "", threeTypes)

	offers, err := a.SearchOffers(context.Background(), OfferQuery{})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}

	want := len(threeTypes) * 2
	if len(offers) != want {
		t.Fatalf("got %d offers, want %d (3 types x 2 zones).\n"+
			"This IS the fallback budget: the launch retry loop can only try as "+
			"many distinct capacity pools as there are offers. Collapsing the "+
			"zone dimension leaves 3 candidates that are all the same pool.",
			len(offers), want)
	}

	// Every offer must be individually launchable: unique id, real subnet.
	ids := map[string]bool{}
	for _, o := range offers {
		if ids[o.ID] {
			t.Errorf("duplicate offer id %q: two zones' candidates are "+
				"indistinguishable in the retry log, which is the one log you read "+
				"after a run of capacity refusals", o.ID)
		}
		ids[o.ID] = true
		if o.PlacementRef == "" {
			t.Errorf("offer %s carries no subnet; the launch would ignore its zone "+
				"entirely and land wherever EC2 chose", o.ID)
		}
		if o.Zone == "" || o.ZoneID == "" {
			t.Errorf("offer %s is missing zone identity (zone=%q zone_id=%q)", o.ID, o.Zone, o.ZoneID)
		}
	}

	// Both subnets must actually appear, or the cross product is a fiction.
	subnets := map[string]int{}
	for _, o := range offers {
		subnets[o.PlacementRef]++
	}
	if subnets["subnet-a"] != 3 || subnets["subnet-b"] != 3 {
		t.Errorf("offers are not evenly spread across subnets: %v", subnets)
	}
}

func TestPerZoneTypeNarrowing(t *testing.T) {
	a := awsWithZones([]AWSZone{
		// The operator knows g6 is never obtainable here and will take a g4dn.
		{Zone: "us-east-2a", ZoneID: "use2-az1", SubnetID: "subnet-a",
			InstanceTypes: []string{"g4dn.xlarge"}},
		// Everything priced is acceptable here.
		{Zone: "us-east-2b", ZoneID: "use2-az2", SubnetID: "subnet-b"},
	}, "", threeTypes)

	offers, err := a.SearchOffers(context.Background(), OfferQuery{})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 4 {
		t.Fatalf("got %d offers, want 4 (1 narrowed + 3 unrestricted)", len(offers))
	}
	for _, o := range offers {
		if o.Zone == "us-east-2a" && o.InstanceType != "g4dn.xlarge" {
			t.Errorf("zone us-east-2a was narrowed to g4dn.xlarge but produced %s.\n"+
				"Per-zone narrowing is how an operator says \"I know that card is "+
				"never available there\"; ignoring it spends retries on pools they "+
				"already told us are empty.", o.InstanceType)
		}
	}
}

func TestEmptyInstanceTypesMeansAllTypesNotNone(t *testing.T) {
	a := awsWithZones([]AWSZone{
		{Zone: "us-east-2a", ZoneID: "use2-az1", SubnetID: "subnet-a", InstanceTypes: []string{}},
	}, "", threeTypes)

	offers, err := a.SearchOffers(context.Background(), OfferQuery{})
	if err != nil {
		t.Fatalf("SearchOffers: %v", err)
	}
	if len(offers) != 3 {
		t.Fatalf("an empty instance_types list produced %d offers, want 3.\n"+
			"Empty means \"every type I have priced\" everywhere in this feature: it "+
			"is what a select-all click stores, and it is what keeps a saved "+
			"selection from silently narrowing when a new type is priced later. "+
			"Reading it as \"none\" disables the zone the operator just ticked.", len(offers))
	}
}

func TestLaunchUsesTheOffersSubnetNotTheGlobalOne(t *testing.T) {
	a := awsWithZones([]AWSZone{
		{Zone: "us-east-2b", ZoneID: "use2-az2", SubnetID: "subnet-b"},
	}, "subnet-legacy", threeTypes)

	in := a.buildRunInput("ami-123", LaunchRequest{
		Label:          "kh-test",
		IdempotencyKey: "kh-test",
		Offer: Offer{
			InstanceType: "g4dn.xlarge",
			Zone:         "us-east-2b",
			PlacementRef: "subnet-b",
		},
		DiskGB: 30,
	}, "#!/bin/bash\ntrue\n")

	if got := aws.ToString(in.SubnetId); got != "subnet-b" {
		t.Fatalf("launch went to subnet %q, want subnet-b.\n"+
			"If the launch ignores the offer's placement, the whole zone dimension "+
			"is decorative: the ranker hands back candidates in three zones and "+
			"all three launches hit the same pool, which is the exact bug this "+
			"work exists to fix.", got)
	}
}

func TestLaunchFallsBackToTheGlobalSubnetForLegacyOffers(t *testing.T) {
	a := awsWithZones(nil, "subnet-legacy", threeTypes)
	in := a.buildRunInput("ami-123", LaunchRequest{
		Label:          "kh-test",
		IdempotencyKey: "kh-test",
		Offer:          Offer{InstanceType: "g4dn.xlarge"}, // no placement
		DiskGB:         30,
	}, "#!/bin/bash\ntrue\n")

	if got := aws.ToString(in.SubnetId); got != "subnet-legacy" {
		t.Fatalf("legacy launch went to subnet %q, want subnet-legacy", got)
	}
}

func TestAvailabilityFromScore(t *testing.T) {
	cases := []struct {
		score int
		want  OfferAvailability
	}{
		{0, AvailabilityUnknown}, // no score is not "no capacity"
		{-1, AvailabilityUnknown},
		{1, AvailabilityLow},
		{3, AvailabilityLow},
		{4, AvailabilityMedium},
		{6, AvailabilityMedium},
		{7, AvailabilityHigh},
		{10, AvailabilityHigh},
	}
	for _, c := range cases {
		if got := availabilityFromScore(c.score); got != c.want {
			t.Errorf("score %d mapped to %s, want %s", c.score, got, c.want)
		}
	}

	if availabilityFromScore(0) != AvailabilityUnknown {
		t.Error("a missing placement score must stay Unknown, never None.\n" +
			"Unknown sorts last but is never filtered; None reads as \"this pool " +
			"is empty\" and an IAM role without ec2:GetSpotPlacementScores would " +
			"then delete every candidate in the search.")
	}
}

/*
 * The ranker tie-break is the other half of multi-zone: three identical offers
 * differing only in zone tie on cost, confidence AND rate, so without an
 * availability tie-break the launch order is decided by the alphabet — 2a
 * first, every time, including when 2a is the exhausted one.
 */
func TestRankerPrefersTheMoreAvailableZoneOnAnOtherwiseExactTie(t *testing.T) {
	offers := []Offer{
		{ID: "g4dn.xlarge@us-east-2a", InstanceType: "g4dn.xlarge", GPUModel: "T4", GPUCount: 1,
			HourlyRateCents: 53, Zone: "us-east-2a", Availability: AvailabilityLow},
		{ID: "g4dn.xlarge@us-east-2c", InstanceType: "g4dn.xlarge", GPUModel: "T4", GPUCount: 1,
			HourlyRateCents: 53, Zone: "us-east-2c", Availability: AvailabilityHigh},
	}

	ranked, _ := rankOffers(RankInput{Provider: models.CloudProviderAWS, Candidates: offers})
	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked offers, got %d", len(ranked))
	}
	if ranked[0].Zone != "us-east-2c" {
		t.Errorf("ranked the LOW-availability zone first (%s).\n"+
			"With cost, confidence and hourly rate identical, the only thing left "+
			"to order these by is the stock signal. Falling through to the "+
			"alphabet means the same zone is always tried first regardless of "+
			"whether AWS just told us it is the emptiest one.", ranked[0].Zone)
	}
}

func TestRankerOrderStaysStableWhenNoZoneHasASignal(t *testing.T) {
	// Every offer Unknown: the availability tie-break must be a no-op, leaving
	// the pre-existing deterministic ID ordering untouched.
	var offers []Offer
	for _, z := range []string{"us-east-2c", "us-east-2a", "us-east-2b"} {
		offers = append(offers, Offer{
			ID: "g4dn.xlarge@" + z, InstanceType: "g4dn.xlarge", GPUModel: "T4", GPUCount: 1,
			HourlyRateCents: 53, Zone: z, Availability: AvailabilityUnknown,
		})
	}
	ranked, _ := rankOffers(RankInput{Provider: models.CloudProviderAWS, Candidates: offers})

	var got []string
	for _, r := range ranked {
		got = append(got, r.ID)
	}
	want := append([]string(nil), got...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order is %v, want the deterministic ID order %v.\n"+
			"A provider that reports no stock signal must rank exactly as it did "+
			"before the availability tie-break existed, or \"why did we rent a "+
			"different box this time\" becomes unanswerable.", got, want)
	}
}

func TestSelectedTypeSetTreatsEmptyAsEverything(t *testing.T) {
	all := []string{"g4dn.xlarge", "g5.xlarge"}

	got := selectedTypeSet(AWSZone{Zone: "us-east-2a"}, all, true)
	if len(got) != 2 {
		t.Errorf("a selected zone with no type list resolved to %d types, want all %d.\n"+
			"The grid would open with every cell unticked for a zone that is, in "+
			"fact, accepting every type — and the operator's first click would "+
			"NARROW their configuration while appearing to widen it.", len(got), len(all))
	}

	if len(selectedTypeSet(AWSZone{Zone: "us-east-2a"}, all, false)) != 0 {
		t.Error("an unselected zone must resolve to no types")
	}

	narrowed := selectedTypeSet(AWSZone{InstanceTypes: []string{"g5.xlarge"}}, all, true)
	if len(narrowed) != 1 || !narrowed["g5.xlarge"] {
		t.Errorf("explicit narrowing was not respected: %v", narrowed)
	}
}

func TestPricedTypesIsSortedAndComplete(t *testing.T) {
	got := awsWithZones(nil, "", threeTypes).pricedTypes()
	want := []string{"g4dn.xlarge", "g5.xlarge", "g6.xlarge"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("pricedTypes() = %v, want %v.\n"+
			"Map iteration order is random in Go, so an unsorted list makes every "+
			"AWS call that takes a type list non-deterministic — including the "+
			"preflight dry run, which would validate a different instance type on "+
			"each attempt.", got, want)
	}
}

/*
 * Observed on a live probe: a single missing ec2:GetSpotPlacementScores
 * permission produced the SAME warning paragraph twice, because the per-cell
 * and per-zone score lookups are separate calls that fail for the same reason.
 *
 * It reads as two distinct problems. An operator who "fixes both" and still
 * sees one learns to distrust the whole warning list — which is the list that
 * also carries "this zone has no subnet" and "your configured rate is below
 * spot".
 */
func TestWarningsAreNotRepeated(t *testing.T) {
	dup := "could not read spot placement scores (403): zones will be tried in name order"
	got := dedupeStrings([]string{dup, "no subnet in us-east-2c", dup})

	if len(got) != 2 {
		t.Fatalf("got %d warnings, want 2: %v", len(got), got)
	}
	if got[0] != dup || got[1] != "no subnet in us-east-2c" {
		t.Errorf("dedupe did not preserve order: %v.\n"+
			"Order matters — the first warning is the one an operator reads.", got)
	}
	// A single warning must pass through untouched, and nil must stay nil
	// rather than becoming an empty slice that renders as an empty alert.
	if got := dedupeStrings(nil); got != nil {
		t.Errorf("dedupeStrings(nil) = %v, want nil", got)
	}
}
