package cloud

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
 * ExploreCapacity answers the question an operator cannot answer from the AWS
 * console without four separate screens: for each availability zone, can I
 * launch there at all, which of my priced instance types does EC2 even sell
 * there, what is spot charging right now, and how does AWS rank the pool?
 *
 * Four calls, joined:
 *
 *	DescribeAvailabilityZones     -> the zones, and their stable zone IDs
 *	DescribeSubnets               -> the subnet each zone needs to be launchable
 *	DescribeInstanceTypeOfferings -> which types EC2 sells in each zone
 *	GetSpotPlacementScores        -> AWS's relative ranking of the pools
 *	DescribeSpotPriceHistory      -> what spot costs there right now
 *
 * PARTIAL FAILURE IS NOT FAILURE. The first two are required — without a zone
 * list and a subnet there is nothing to show — but a missing offering list,
 * price or score degrades a cell to "unknown" and names the gap in Warnings.
 * An operator whose IAM role lacks ec2:GetSpotPlacementScores should still be
 * able to pick zones; refusing the whole screen over an advisory signal would
 * make the permission effectively mandatory.
 */
func (a *AWSProvider) ExploreCapacity(ctx context.Context) (*CapacityReport, error) {
	report := &CapacityReport{
		Region:         a.settings.Region,
		PlacementLabel: "Availability zone",
		HardwareLabel:  "Instance type",
		Hardware:       a.pricedTypes(),
		Signals:        awsSignals(a.settings.UseSpot),
	}
	if len(report.Hardware) == 0 {
		report.Warnings = append(report.Warnings,
			"no instance_type_rates configured: there is nothing to check availability for, and no launch can be priced")
	}

	azOut, err := a.ec2.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{})
	if err != nil {
		return nil, fmt.Errorf("aws: DescribeAvailabilityZones: %w", err)
	}

	// What is configured today, so the screen opens on the live state. Keyed by
	// zone NAME because that is what an operator ticks, with the zone ID as a
	// fallback for a config written before names were recorded.
	selectedZones := make(map[string]AWSZone, len(a.settings.Zones))
	for _, z := range a.settings.Zones {
		if z.Zone != "" {
			selectedZones[z.Zone] = z
		}
		if z.ZoneID != "" {
			selectedZones[z.ZoneID] = z
		}
	}
	// The legacy single subnet counts as a selection, or an operator opening
	// this screen on an existing config would see their live placement
	// unticked and conclude nothing was configured.
	legacySubnet := a.settings.SubnetID

	subnets, subnetWarn := a.subnetsByZone(ctx)
	report.Warnings = append(report.Warnings, subnetWarn...)

	offerings, offerWarn := a.offeringsByZone(ctx, report.Hardware)
	report.Warnings = append(report.Warnings, offerWarn...)

	// Per-CELL scores here, unlike the launch path: the grid is asking "how
	// obtainable is this card in this zone?", which needs one call per type.
	// See spotScoresPerType for why a single multi-type call cannot answer it.
	var cellScores map[string]int
	var zoneScores map[string]int
	var prices map[string]spotQuote
	if a.settings.UseSpot {
		var cellWarn, zoneWarn, priceWarn []string
		cellScores, cellWarn = a.spotScoresPerType(ctx, report.Hardware)
		zoneScores, zoneWarn = a.spotPlacementScoresVerbose(ctx, report.Hardware)
		prices, priceWarn = a.spotPricesByZone(ctx, report.Hardware)
		report.Warnings = append(report.Warnings, cellWarn...)
		report.Warnings = append(report.Warnings, zoneWarn...)
		report.Warnings = append(report.Warnings, priceWarn...)
	}

	for _, az := range azOut.AvailabilityZones {
		name := aws.ToString(az.ZoneName)
		zoneID := aws.ToString(az.ZoneId)

		// ID is the zone ID and Name the alias, never the other way round: the
		// alias is per-account, so anything joined against a capacity API on it
		// would silently match another account's datacentre.
		zc := PlacementCapacity{
			ID:     zoneID,
			Name:   name,
			Usable: true,
		}
		if az.State != ec2types.AvailabilityZoneStateAvailable {
			zc.Usable = false
			zc.Notes = append(zc.Notes, fmt.Sprintf(
				"zone state is %q, not \"available\": EC2 will not launch here", az.State))
		}
		// "opted-in" and "opt-in-not-required" are both fine. Anything else is
		// a Local Zone or Wavelength zone the account has not enabled, which
		// fails at launch with an unhelpful error.
		if s := string(az.OptInStatus); s != "opt-in-not-required" && s != "opted-in" {
			zc.Usable = false
			zc.Notes = append(zc.Notes, fmt.Sprintf(
				"opt-in status is %q: enable this zone in the EC2 console before selecting it", s))
		}

		if sn, ok := subnets[name]; ok {
			zc.Ref = sn.id
			zc.Detail = subnetDetail(sn)
			if sn.availableIPs == 0 {
				zc.Usable = false
				zc.Notes = append(zc.Notes,
					"subnet has no free IP addresses; a launch here will fail with InsufficientFreeAddressesInSubnet")
			}
		} else if zc.Usable {
			zc.Usable = false
			zc.Notes = append(zc.Notes,
				"no subnet in this zone: create one in the VPC used by the other zones before selecting it")
		}

		if sel, ok := selectedZones[name]; ok {
			zc.Selected = true
			// A configured zone whose recorded subnet no longer resolves is
			// worth saying out loud: it is live configuration that will fail.
			if sel.SubnetID != "" && sel.SubnetID != zc.Ref {
				zc.Ref = sel.SubnetID
				zc.Notes = append(zc.Notes, fmt.Sprintf(
					"configured subnet %s was not found in this zone; it may have been deleted", sel.SubnetID))
			}
		} else if legacySubnet != "" && subnets[name].id == legacySubnet {
			zc.Selected = true
			zc.Notes = append(zc.Notes,
				"selected via the legacy single subnet_id setting; saving this screen converts it to a zone entry")
		}

		selectedTypes := selectedTypeSet(selectedZones[name], report.Hardware, zc.Selected)

		for _, t := range report.Hardware {
			cell := HardwareCapacity{
				ID:              t,
				ConfiguredCents: a.settings.InstanceTypeRates[t],
				Selected:        zc.Selected && selectedTypes[t],
				Score:           cellScores[zoneID+"|"+t],
			}
			if offerings != nil {
				cell.Offered = offerings[name][t]
			} else {
				// Unknown, not false. Rendering "not offered" because a
				// describe call failed would hide capacity that exists.
				cell.Offered = true
			}
			if hw, ok := a.settings.InstanceTypeGPUs[t]; ok {
				cell.GPUModel = hw.GPUModel
				cell.GPUCount = hw.GPUCount
			}
			if q, ok := prices[zoneID+"|"+t]; ok {
				cell.LiveCents = q.cents
				cell.ObservedAt = q.observedAt
			}
			zc.Hardware = append(zc.Hardware, cell)
			if cell.Selected && cell.Offered {
				report.PoolCount++
			}
		}

		zc.Score = zoneScores[zoneID]
		report.Placements = append(report.Placements, zc)
	}

	sort.Slice(report.Placements, func(i, j int) bool {
		return report.Placements[i].Name < report.Placements[j].Name
	})

	/*
	 * Dedupe. The per-cell and per-zone score lookups are separate calls that
	 * fail for the same reason, so a missing ec2:GetSpotPlacementScores
	 * permission produced the SAME paragraph twice — once from each. Observed
	 * on a live probe.
	 *
	 * Worth more than tidiness: this warning is long and names an IAM action,
	 * and seeing it twice reads as two distinct problems. An operator who fixes
	 * "both" and still sees one has learned to distrust the whole list.
	 */
	report.Warnings = dedupeStrings(report.Warnings)

	if report.PoolCount == 1 {
		report.Warnings = append(report.Warnings,
			"only one capacity pool is selected. EC2 spot capacity is a property of the (instance type, zone) pair, "+
				"so a single pool gives a launch exactly one chance: an InsufficientInstanceCapacity refusal has "+
				"nothing to fall back to. Select more zones, more instance types, or both.")
	}
	return report, nil
}

/*
 * selectedTypeSet resolves which types are ticked for a zone.
 *
 * The empty InstanceTypes list means "all of them" throughout — it is what a
 * select-all click produces and what keeps a config from silently narrowing
 * when the operator later prices a new instance type. Rendering it as nothing
 * selected would invert that meaning on screen.
 */
func selectedTypeSet(z AWSZone, all []string, zoneSelected bool) map[string]bool {
	out := make(map[string]bool, len(all))
	if !zoneSelected {
		return out
	}
	if len(z.InstanceTypes) == 0 {
		for _, t := range all {
			out[t] = true
		}
		return out
	}
	for _, t := range z.InstanceTypes {
		out[t] = true
	}
	return out
}

type subnetInfo struct {
	id           string
	name         string
	availableIPs int
}

// subnetDetail is the one-line context shown under a zone name. The free-address
// count belongs here rather than in a note: it is fine until it is zero, and an
// operator choosing between zones wants to see it before it becomes a problem.
func subnetDetail(sn subnetInfo) string {
	if sn.name != "" {
		return fmt.Sprintf("%s (%s) · %d free IPs", sn.id, sn.name, sn.availableIPs)
	}
	return fmt.Sprintf("%s · %d free IPs", sn.id, sn.availableIPs)
}

/*
 * awsSignals declares which grid columns AWS can fill and how far each should
 * be trusted.
 *
 * The trust levels are not decoration. AWS is the provider whose headline
 * capacity signal is worth the least — it has no stock count at all, and its
 * placement score has been measured contradicting reality — while its Offered
 * flag is the strongest signal any provider gives. Rendering those two columns
 * with equal weight is how an operator ends up unticking a zone that would have
 * worked.
 */
func awsSignals(useSpot bool) []SignalDescriptor {
	sigs := []SignalDescriptor{{
		Kind:  SignalOffered,
		Label: "Offered",
		Trust: TrustDefinitive,
		Explanation: "DescribeInstanceTypeOfferings: whether EC2 sells this instance type in this " +
			"zone at all. A hard no — a launch here would fail with Unsupported.",
	}, {
		Kind:  SignalConfiguredPrice,
		Label: "Your rate",
		Trust: TrustDefinitive,
		Explanation: "Your own instance_type_rates figure. This is what the budget reserves " +
			"against, so it is the number that makes a spend cap real — not the live price.",
	}}
	if !useSpot {
		return sigs
	}
	return append(sigs, SignalDescriptor{
		Kind:  SignalLivePrice,
		Label: "Spot now",
		Trust: TrustMeasured,
		Explanation: "The live spot price. Compare it with your configured rate: pricing above " +
			"the live rate only over-reserves, but pricing BELOW it under-reserves against a cap " +
			"you were told was hard.",
	}, SignalDescriptor{
		Kind:  SignalScore,
		Label: "Score",
		Trust: TrustAdvisory,
		Explanation: "AWS's own 1-10 ranking of this pool, and weak evidence: every zone in a " +
			"region scored 1/10 on a live account minutes before a launch in one of them succeeded " +
			"on the first try. It decides which pool is tried first. It never removes one.",
	})
}

/*
 * subnetsByZone picks ONE subnet per zone: the launch target.
 *
 * Where a zone has several, the one with the most free addresses wins, with the
 * id as a deterministic tie-break. This is a guess, and a reasonable one — but
 * it is a guess about network topology, so the chosen subnet is always shown to
 * the operator rather than applied silently, and they can override it.
 */
func (a *AWSProvider) subnetsByZone(ctx context.Context) (map[string]subnetInfo, []string) {
	out := map[string]subnetInfo{}

	in := &ec2.DescribeSubnetsInput{}
	// Scope to the VPC the current configuration already uses, so a multi-VPC
	// account does not offer subnets the security groups cannot attach to.
	if vpcID := a.vpcForConfiguredSubnet(ctx); vpcID != "" {
		in.Filters = []ec2types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}}
	}

	sn, err := a.ec2.DescribeSubnets(ctx, in)
	if err != nil {
		return out, []string{fmt.Sprintf(
			"could not list subnets (%v): zones cannot be shown as launchable without one, "+
				"and ec2:DescribeSubnets is required for this screen", err)}
	}

	for _, s := range sn.Subnets {
		zone := aws.ToString(s.AvailabilityZone)
		info := subnetInfo{
			id:           aws.ToString(s.SubnetId),
			availableIPs: int(aws.ToInt32(s.AvailableIpAddressCount)),
		}
		for _, tag := range s.Tags {
			if aws.ToString(tag.Key) == "Name" {
				info.name = aws.ToString(tag.Value)
			}
		}
		prev, exists := out[zone]
		if !exists || info.availableIPs > prev.availableIPs ||
			(info.availableIPs == prev.availableIPs && info.id < prev.id) {
			out[zone] = info
		}
	}
	return out, nil
}

// vpcForConfiguredSubnet resolves the VPC of whichever subnet is already in
// use, so discovery stays inside it. Returns "" when nothing is configured yet,
// which correctly means "show me everything".
func (a *AWSProvider) vpcForConfiguredSubnet(ctx context.Context) string {
	ref := a.settings.SubnetID
	for _, z := range a.settings.Zones {
		if z.SubnetID != "" {
			ref = z.SubnetID
			break
		}
	}
	if ref == "" {
		return ""
	}
	out, err := a.ec2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{SubnetIds: []string{ref}})
	if err != nil || len(out.Subnets) == 0 {
		debug.Warning("Cloud/AWS: could not resolve VPC for configured subnet %s: %v", ref, err)
		return ""
	}
	return aws.ToString(out.Subnets[0].VpcId)
}

/*
 * offeringsByZone reports which of the given instance types EC2 sells in each
 * zone. This is the ONLY definitive signal on the whole screen: a type not
 * offered in a zone can never launch there, no matter what price or score says.
 *
 * Returns nil (not an empty map) when the call fails, so callers can tell
 * "nothing is offered" apart from "we could not find out" — the difference
 * between an empty grid and a wrong one.
 */
func (a *AWSProvider) offeringsByZone(ctx context.Context, types []string) (map[string]map[string]bool, []string) {
	if len(types) == 0 {
		return map[string]map[string]bool{}, nil
	}

	filters := []ec2types.Filter{{Name: aws.String("instance-type"), Values: types}}
	out := map[string]map[string]bool{}

	p := ec2.NewDescribeInstanceTypeOfferingsPaginator(a.ec2, &ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: ec2types.LocationTypeAvailabilityZone,
		Filters:      filters,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, []string{fmt.Sprintf(
				"could not read instance type offerings (%v): the grid cannot show which types EC2 sells in "+
					"which zone, so every cell is shown as available and a launch may fail with "+
					"Unsupported instead", err)}
		}
		for _, o := range page.InstanceTypeOfferings {
			zone := aws.ToString(o.Location)
			if out[zone] == nil {
				out[zone] = map[string]bool{}
			}
			out[zone][string(o.InstanceType)] = true
		}
	}
	return out, nil
}

type spotQuote struct {
	cents      int
	observedAt time.Time
}

/*
 * spotPricesByZone reads the current spot rate for each (zone, type) pair,
 * keyed "zoneID|instanceType".
 *
 * Shown next to the operator's configured rate because the two drift and only
 * one of them is real. Reservations are denominated in the CONFIGURED figure,
 * so pricing above the live rate merely over-reserves; pricing BELOW it
 * under-reserves a cap the operator was told was hard. Seeing both numbers side
 * by side is the only way to notice.
 */
func (a *AWSProvider) spotPricesByZone(ctx context.Context, types []string) (map[string]spotQuote, []string) {
	out := map[string]spotQuote{}
	if len(types) == 0 {
		return out, nil
	}

	// StartTime = now asks for the CURRENT price rather than a history window.
	res, err := a.ec2.DescribeSpotPriceHistory(ctx, &ec2.DescribeSpotPriceHistoryInput{
		InstanceTypes:       toInstanceTypes(types),
		ProductDescriptions: []string{"Linux/UNIX"},
		StartTime:           aws.Time(time.Now()),
		MaxResults:          aws.Int32(200),
	})
	if err != nil {
		return out, []string{fmt.Sprintf(
			"could not read spot prices (%v): the grid will show your configured rates only, "+
				"with nothing to check them against", err)}
	}

	// Zone NAME is what this API returns; the grid is keyed by zone ID, so
	// translate through the AZ list rather than assuming they correspond.
	nameToID, warn := a.zoneNameToID(ctx)

	for _, p := range res.SpotPriceHistory {
		usd, perr := strconv.ParseFloat(aws.ToString(p.SpotPrice), 64)
		if perr != nil || usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
			continue
		}
		zoneID := nameToID[aws.ToString(p.AvailabilityZone)]
		if zoneID == "" {
			continue
		}
		key := zoneID + "|" + string(p.InstanceType)
		// Round UP: a price shown low is a price an operator budgets low.
		q := spotQuote{cents: int(math.Ceil(usd * 100)), observedAt: aws.ToTime(p.Timestamp)}
		if prev, ok := out[key]; ok && !prev.observedAt.Before(q.observedAt) {
			continue
		}
		out[key] = q
	}
	return out, warn
}

func (a *AWSProvider) zoneNameToID(ctx context.Context) (map[string]string, []string) {
	out := map[string]string{}
	az, err := a.ec2.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{})
	if err != nil {
		return out, []string{fmt.Sprintf("could not map zone names to zone IDs (%v); spot prices omitted", err)}
	}
	for _, z := range az.AvailabilityZones {
		out[aws.ToString(z.ZoneName)] = aws.ToString(z.ZoneId)
	}
	return out, nil
}

func toInstanceTypes(types []string) []ec2types.InstanceType {
	out := make([]ec2types.InstanceType, 0, len(types))
	for _, t := range types {
		out = append(out, ec2types.InstanceType(t))
	}
	return out
}

// spotPlacementScores is the quiet variant used on the launch path, where a
// failure must cost ordering and nothing else.
func (a *AWSProvider) spotPlacementScores(ctx context.Context, types []string) map[string]int {
	scores, warnings := a.spotPlacementScoresVerbose(ctx, types)
	for _, w := range warnings {
		debug.Debug("Cloud/AWS: %s", w)
	}
	return scores
}

/*
 * spotScoresPerType scores each (zone, type) pair individually, keyed
 * "zoneID|instanceType". One AWS call per instance type.
 *
 * THE EXTRA CALLS ARE THE POINT. GetSpotPlacementScores scores the SET of
 * instance types it is given, not each one — pass three types and you get one
 * number per zone meaning "how easily could you get ANY of these". Measured on
 * this account, same region, same minute:
 *
 *	g4dn+g5+g6 together : az1=9  az2=9  az3=9
 *	g4dn.xlarge alone   : az1=3  az2=3  az3=3
 *	g5.xlarge alone     : az1=1  az2=3  az3=3
 *	g6.xlarge alone     : az1=1  az2=3  az3=1
 *
 * Painting the combined 9 into every cell would tell an operator that g6 in az1
 * is a safe bet when AWS is in fact scoring it 1. The grid exists so they can
 * choose per card; it has to ask per card.
 *
 * That table is also the whole feature in one measurement: breadth IS
 * availability, and AWS scores it that way. Three types together score 9 in a
 * zone where the individual cards score 1 to 3.
 */
func (a *AWSProvider) spotScoresPerType(ctx context.Context, types []string) (map[string]int, []string) {
	out := map[string]int{}
	var warnings []string
	for _, t := range types {
		scores, warn := a.spotPlacementScoresVerbose(ctx, []string{t})
		if len(warn) > 0 {
			// One warning is enough; N identical ones per type is noise.
			if len(warnings) == 0 {
				warnings = warn
			}
			continue
		}
		for zoneID, score := range scores {
			out[zoneID+"|"+t] = score
		}
	}
	return out, warnings
}

/*
 * spotPlacementScoresVerbose asks AWS to rank the pools for the GIVEN SET of
 * instance types, keyed by zone ID.
 *
 * On the launch path this set semantics is exactly right: the question there is
 * "given I will accept any of these types, which zone should I try first", and
 * the cost ranker already handles choosing between types. For the per-card view
 * the picker needs, use spotScoresPerType.
 *
 * READ THE SCORE AS A HINT AND NOTHING MORE. Measured on this account: every
 * us-east-2 zone scored 1/10 for a single instance type minutes before a launch
 * in one of them succeeded on the first attempt. The score reflects AWS's view
 * of a hypothetical fleet request, not whether one instance can be had. It is
 * used to decide which pool to try FIRST, never to remove a pool from the list
 * — gating on it would have refused a launch that worked.
 *
 * SingleAvailabilityZone asks for per-AZ rather than per-region scores, and the
 * response carries AvailabilityZoneId, which is why the whole grid joins on
 * zone ID: the ZoneName alias differs per account and would mismatch.
 */
func (a *AWSProvider) spotPlacementScoresVerbose(ctx context.Context, types []string) (map[string]int, []string) {
	out := map[string]int{}
	if len(types) == 0 {
		return out, nil
	}

	res, err := a.ec2.GetSpotPlacementScores(ctx, &ec2.GetSpotPlacementScoresInput{
		InstanceTypes:          types,
		TargetCapacity:         aws.Int32(1),
		SingleAvailabilityZone: aws.Bool(true),
		RegionNames:            []string{a.settings.Region},
	})
	if err != nil {
		return out, []string{fmt.Sprintf(
			"could not read spot placement scores (%v): zones will be tried in name order rather than "+
				"likeliest-first. This is advisory only and does not stop any launch; grant "+
				"ec2:GetSpotPlacementScores to enable it", err)}
	}

	for _, s := range res.SpotPlacementScores {
		id := aws.ToString(s.AvailabilityZoneId)
		if id == "" || s.Score == nil {
			continue
		}
		if v := int(aws.ToInt32(s.Score)); v > out[id] {
			out[id] = v
		}
	}
	return out, nil
}
