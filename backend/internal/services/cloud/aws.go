package cloud

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	sqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// Tag keys. Everything we create carries TagManaged; nothing without it is
// ever terminated, and the IAM policy is written to enforce the same rule
// server-side so a bug here cannot reach another team's instances.
const (
	TagManaged  = "krakenhashes:managed"
	TagLabel    = "krakenhashes:label"
	TagJobID    = "krakenhashes:job-id"
	TagTTLEpoch = "krakenhashes:ttl-epoch"
)

// Service quota codes for GPU instances. Both default to ZERO on every AWS
// account, which is the single most common reason a first launch fails.
const (
	QuotaCodeOnDemandG = "L-DB2E81BA" // Running On-Demand G and VT instances (vCPUs)
	QuotaCodeSpotG     = "L-3819A6DF" // All G and VT Spot Instance Requests (vCPUs)
)

// AWSSettings is the non-secret provider configuration.
type AWSSettings struct {
	Region             string   `json:"region"`
	SubnetID           string   `json:"subnet_id"`
	SecurityGroupIDs   []string `json:"security_group_ids"`
	IAMInstanceProfile string   `json:"iam_instance_profile_arn"`
	// AMIID pins an image. When empty, AMISSMParameter is resolved instead.
	AMIID string `json:"ami_id"`
	// AMISSMParameter is an SSM public parameter path, e.g.
	// /aws/service/deeplearning/ami/x86_64/base-oss-nvidia-driver-gpu-ubuntu-22.04/latest/ami-id
	// Resolving through SSM rather than matching an AMI name avoids the
	// brittle name-glob lookup that breaks whenever AWS renames an image.
	AMISSMParameter string `json:"ami_ssm_parameter"`
	// UseSpot requests spot capacity. Spot discounts on current-gen GPU are
	// often only 3-40%, so this is not automatically the cheaper choice.
	UseSpot bool `json:"use_spot"`
	// InstanceTypeRates maps an instance type to its hourly cost in cents.
	// Operator-supplied rather than resolved from the Pricing API: that API
	// needs exact capacitystatus/preInstalledSw/tenancy filters or it silently
	// returns the wrong SKU, and the operator already knows their real rates.
	InstanceTypeRates map[string]int `json:"instance_type_rates"`
	// RootVolumeGB is a floor; the job's file set may require more.
	RootVolumeGB int `json:"root_volume_gb"`
	// EBSCentsPerGBMonth is the gp3 storage rate. Left at zero, EBS is simply
	// never budgeted: Offer.StorageCentsPerHour stays 0, PlanLaunch's extraCents
	// term is 0, and the reservation covers only compute. A 500GB volume on a
	// long job is real money to be wrong about, and being wrong in this
	// direction means over-committing the client's cap.
	//
	// Operator-supplied for the same reason as InstanceTypeRates, and it should
	// be rounded UP: reservations are denominated in these declared cents and
	// nothing cross-checks them against a real invoice.
	EBSCentsPerGBMonth float64 `json:"ebs_cents_per_gb_month"`
}

// defaultEBSCentsPerGBMonth is us-east-1 gp3 at $0.08/GB-month. A wrong-region
// default is better than silently budgeting zero for storage, and the operator
// can override it in settings.
const defaultEBSCentsPerGBMonth = 8.0

// hoursPerMonth is AWS's own billing convention (730), not 720 or 744.
const hoursPerMonth = 730

/*
 * ebsCentsPerHour converts an EBS volume size into the hourly rate the budget
 * reserves against.
 *
 * Rounds UP, always. A reservation that under-states storage lets a client
 * exceed a cap they were told was hard, which is the failure that matters here;
 * over-reserving by a cent an hour only means renting slightly less.
 */
func (a *AWSProvider) ebsCentsPerHour(diskGB int) int {
	if diskGB <= 0 {
		return 0
	}
	rate := a.settings.EBSCentsPerGBMonth
	if rate <= 0 {
		rate = defaultEBSCentsPerGBMonth
	}
	perHour := (float64(diskGB) * rate) / hoursPerMonth
	cents := int(perHour)
	if perHour > float64(cents) {
		cents++
	}
	return cents
}

// AWSCredentials is the encrypted secret blob.
type AWSCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
	// RoleARN, when set, is assumed after the base credentials resolve.
	RoleARN string `json:"role_arn,omitempty"`
}

/*
 * AWSProvider launches EC2 GPU instances.
 *
 * Two ideas that sound right for bounding cost are NOT available here, and the
 * code deliberately does not attempt them:
 *
 *   - Spot ValidUntil is documented as supported only for PERSISTENT requests.
 *     We use one-time requests, so it cannot bound runtime.
 *   - BlockDurationMinutes (spot blocks) is marked Deprecated in the SDK.
 *
 * What actually bounds an instance is InstanceInitiatedShutdownBehavior set to
 * terminate, combined with the absolute deadline armed inside the guest. That
 * pair survives this process, this host, and the network all disappearing.
 */
type AWSProvider struct {
	settings AWSSettings
	ec2      *ec2.Client
	sts      *sts.Client
	quotas   *servicequotas.Client
	ssm      *ssm.Client
}

// NewAWSProvider builds a provider from decrypted credentials and settings.
func NewAWSProvider(ctx context.Context, settings AWSSettings, creds AWSCredentials) (*AWSProvider, error) {
	if settings.Region == "" {
		return nil, fmt.Errorf("aws: region is required")
	}

	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(settings.Region)}
	if creds.AccessKeyID != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws: load config: %w", err)
	}

	return &AWSProvider{
		settings: settings,
		ec2:      ec2.NewFromConfig(cfg),
		sts:      sts.NewFromConfig(cfg),
		quotas:   servicequotas.NewFromConfig(cfg),
		ssm:      ssm.NewFromConfig(cfg),
	}, nil
}

// Name identifies the provider.
func (a *AWSProvider) Name() models.CloudProvider { return models.CloudProviderAWS }

/*
 * Preflight answers "will a launch work?" without spending anything.
 *
 * Three independent layers, because each catches what the others miss:
 *   1. STS GetCallerIdentity — do credentials resolve at all
 *   2. Service Quotas — GPU quotas default to 0; DryRun does NOT check quota,
 *      so this is the only way to catch the most common failure
 *   3. DryRun RunInstances — the ground truth on IAM and on the exact
 *      parameter set we would really send
 */
func (a *AWSProvider) Preflight(ctx context.Context) (*PreflightReport, error) {
	r := &PreflightReport{}

	ident, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("sts:GetCallerIdentity failed: %v", err))
		return r, nil
	}
	r.Identity = aws.ToString(ident.Arn)

	// Quota. GetServiceQuota returns NoSuchResourceException when the quota
	// has never been adjusted, which means "still at the AWS default" — not
	// zero-and-unusable — so fall back rather than reporting a hard failure.
	quotaCode := QuotaCodeOnDemandG
	if a.settings.UseSpot {
		quotaCode = QuotaCodeSpotG
	}
	limit, source, qErr := a.readQuota(ctx, quotaCode)
	if qErr != nil {
		r.Inconclusive = append(r.Inconclusive, fmt.Sprintf("could not read quota %s: %v", quotaCode, qErr))
	} else {
		r.QuotaLimit = limit
		r.QuotaSource = source
		if limit <= 0 {
			r.Errors = append(r.Errors, fmt.Sprintf(
				"quota %s is %.0f vCPUs: this account cannot launch ANY GPU instance until an increase is granted",
				quotaCode, limit))
		}
	}

	used, uErr := a.usedGPUVCPUs(ctx)
	if uErr != nil {
		r.Inconclusive = append(r.Inconclusive, fmt.Sprintf("could not read current usage: %v", uErr))
	} else {
		r.QuotaUsed = used
	}

	// DryRun the real call shape.
	if a.settings.AMIID == "" && a.settings.AMISSMParameter == "" {
		r.Errors = append(r.Errors, "neither ami_id nor ami_ssm_parameter is configured")
	} else if _, err := a.resolveAMI(ctx); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("could not resolve AMI: %v", err))
	} else if err := a.dryRunLaunch(ctx); err != nil {
		var missing string
		if isUnauthorized(err) {
			missing = "ec2:RunInstances (or one of the resource ARNs it touches: image, subnet, security-group, instance, volume, network-interface)"
			r.MissingPermissions = append(r.MissingPermissions, missing)
		} else {
			r.Errors = append(r.Errors, fmt.Sprintf("RunInstances dry run failed: %v", err))
		}
	}

	r.Warnings = append(r.Warnings,
		"DryRun validates IAM and parameters but NOT quota or capacity; a launch can still fail with VcpuLimitExceeded or InsufficientInstanceCapacity")

	r.OK = len(r.Errors) == 0 && len(r.MissingPermissions) == 0 && len(r.Inconclusive) == 0
	return r, nil
}

func (a *AWSProvider) readQuota(ctx context.Context, code string) (float64, string, error) {
	out, err := a.quotas.GetServiceQuota(ctx, &servicequotas.GetServiceQuotaInput{
		ServiceCode: aws.String("ec2"),
		QuotaCode:   aws.String(code),
	})
	if err == nil && out.Quota != nil && out.Quota.Value != nil {
		return *out.Quota.Value, "applied", nil
	}

	var notFound *sqtypes.NoSuchResourceException
	if errors.As(err, &notFound) || err != nil {
		def, dErr := a.quotas.GetAWSDefaultServiceQuota(ctx, &servicequotas.GetAWSDefaultServiceQuotaInput{
			ServiceCode: aws.String("ec2"),
			QuotaCode:   aws.String(code),
		})
		if dErr != nil {
			return 0, "", fmt.Errorf("applied: %v; default: %w", err, dErr)
		}
		if def.Quota != nil && def.Quota.Value != nil {
			return *def.Quota.Value, "aws-default", nil
		}
	}
	return 0, "", fmt.Errorf("quota %s unavailable", code)
}

// usedGPUVCPUs sums vCPUs of running G/VT instances. DescribeInstances is the
// immediate, authoritative source; CloudWatch AWS/Usage lags by minutes and is
// only suitable for dashboards.
func (a *AWSProvider) usedGPUVCPUs(ctx context.Context) (float64, error) {
	out, err := a.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("instance-state-name"), Values: []string{"pending", "running"}},
		},
	})
	if err != nil {
		return 0, err
	}

	var total float64
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			t := string(inst.InstanceType)
			if !strings.HasPrefix(t, "g") && !strings.HasPrefix(t, "vt") {
				continue
			}
			if inst.CpuOptions != nil && inst.CpuOptions.CoreCount != nil && inst.CpuOptions.ThreadsPerCore != nil {
				total += float64(*inst.CpuOptions.CoreCount * *inst.CpuOptions.ThreadsPerCore)
			}
		}
	}
	return total, nil
}

// resolveAMI returns the AMI to launch, preferring an explicit pin.
func (a *AWSProvider) resolveAMI(ctx context.Context) (string, error) {
	if a.settings.AMIID != "" {
		return a.settings.AMIID, nil
	}
	out, err := a.ssm.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(a.settings.AMISSMParameter),
	})
	if err != nil {
		return "", fmt.Errorf("resolve AMI from SSM parameter %q: %w", a.settings.AMISSMParameter, err)
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("SSM parameter %q returned no value", a.settings.AMISSMParameter)
	}
	return *out.Parameter.Value, nil
}

// SearchOffers turns the operator's configured instance types into offers.
// AWS has no marketplace to query: the "offer" is a type at a known rate.
func (a *AWSProvider) SearchOffers(ctx context.Context, q OfferQuery) ([]Offer, error) {
	if len(a.settings.InstanceTypeRates) == 0 {
		return nil, fmt.Errorf("aws: no instance_type_rates configured")
	}

	allowed := make(map[string]bool, len(q.AllowedInstanceTypes))
	for _, t := range q.AllowedInstanceTypes {
		allowed[t] = true
	}

	// The volume that will actually be attached: the job's file set, floored at
	// the configured root size. Budgeting the floor when the job needs more
	// would under-reserve exactly on the big jobs where it matters.
	diskGB := q.MinDiskGB
	if a.settings.RootVolumeGB > diskGB {
		diskGB = a.settings.RootVolumeGB
	}
	storageCentsPerHour := a.ebsCentsPerHour(diskGB)

	var offers []Offer
	for instType, cents := range a.settings.InstanceTypeRates {
		if len(allowed) > 0 && !allowed[instType] {
			continue
		}
		if q.MaxHourlyRateCents > 0 && cents > q.MaxHourlyRateCents {
			continue
		}
		offers = append(offers, Offer{
			ID:                  instType,
			InstanceType:        instType,
			GPUModel:            instType,
			GPUCount:            1,
			HourlyRateCents:     cents,
			StorageCentsPerHour: storageCentsPerHour,
			Region:              a.settings.Region,
			Raw:                 models.JSONMap{"instance_type": instType},
		})
	}
	if len(offers) == 0 {
		return nil, fmt.Errorf("aws: no configured instance type satisfies the request")
	}

	// Cheapest first, so the caller's first choice is the cheapest viable one.
	for i := 0; i < len(offers); i++ {
		for j := i + 1; j < len(offers); j++ {
			if offers[j].HourlyRateCents < offers[i].HourlyRateCents {
				offers[i], offers[j] = offers[j], offers[i]
			}
		}
	}
	return offers, nil
}

// buildRunInput assembles the exact RunInstances call, shared by Launch and
// the dry run so preflight validates what production actually sends.
func (a *AWSProvider) buildRunInput(amiID string, req LaunchRequest, userData string) *ec2.RunInstancesInput {
	tags := []ec2types.Tag{
		{Key: aws.String(TagManaged), Value: aws.String("true")},
		{Key: aws.String(TagLabel), Value: aws.String(req.Label)},
		{Key: aws.String("Name"), Value: aws.String(req.Label)},
	}
	if req.TTL > 0 {
		tags = append(tags, ec2types.Tag{
			Key:   aws.String(TagTTLEpoch),
			Value: aws.String(strconv.FormatInt(time.Now().Add(req.TTL).Unix(), 10)),
		})
	}
	if jobID := req.Env["KH_JOB_ID"]; jobID != "" {
		tags = append(tags, ec2types.Tag{Key: aws.String(TagJobID), Value: aws.String(jobID)})
	}

	// Tag every created resource type. An untagged EBS volume is the classic
	// orphan: it survives the instance and bills indefinitely.
	tagSpecs := []ec2types.TagSpecification{
		{ResourceType: ec2types.ResourceTypeInstance, Tags: tags},
		{ResourceType: ec2types.ResourceTypeVolume, Tags: tags},
		{ResourceType: ec2types.ResourceTypeNetworkInterface, Tags: tags},
	}

	diskGB := req.DiskGB
	if diskGB < a.settings.RootVolumeGB {
		diskGB = a.settings.RootVolumeGB
	}
	if diskGB < 30 {
		diskGB = 30
	}

	in := &ec2.RunInstancesInput{
		ImageId:      aws.String(amiID),
		InstanceType: ec2types.InstanceType(req.Offer.InstanceType),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		// Deterministic per instance. A random token per retry would let an
		// SDK-level retry after a network timeout launch a SECOND paid GPU.
		ClientToken: aws.String(req.IdempotencyKey),
		// Default is "stop", which for an ephemeral worker means paying for
		// EBS forever. This plus the in-guest deadline is the real kill path.
		InstanceInitiatedShutdownBehavior: ec2types.ShutdownBehaviorTerminate,
		UserData:                          aws.String(base64.StdEncoding.EncodeToString([]byte(userData))),
		TagSpecifications:                 tagSpecs,
		MetadataOptions: &ec2types.InstanceMetadataOptionsRequest{
			HttpTokens: ec2types.HttpTokensStateRequired,
		},
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{{
			DeviceName: aws.String("/dev/sda1"),
			Ebs: &ec2types.EbsBlockDevice{
				VolumeSize: aws.Int32(int32(diskGB)),
				VolumeType: ec2types.VolumeTypeGp3,
				// Without this the volume outlives the instance and bills on.
				DeleteOnTermination: aws.Bool(true),
				Encrypted:           aws.Bool(true),
			},
		}},
	}

	if a.settings.SubnetID != "" {
		in.SubnetId = aws.String(a.settings.SubnetID)
	}
	if len(a.settings.SecurityGroupIDs) > 0 {
		in.SecurityGroupIds = a.settings.SecurityGroupIDs
	}
	if a.settings.IAMInstanceProfile != "" {
		in.IamInstanceProfile = &ec2types.IamInstanceProfileSpecification{
			Arn: aws.String(a.settings.IAMInstanceProfile),
		}
	}

	if a.settings.UseSpot {
		in.TagSpecifications = append(in.TagSpecifications, ec2types.TagSpecification{
			ResourceType: ec2types.ResourceTypeSpotInstancesRequest, Tags: tags,
		})
		in.InstanceMarketOptions = &ec2types.InstanceMarketOptionsRequest{
			MarketType: ec2types.MarketTypeSpot,
			SpotOptions: &ec2types.SpotMarketOptions{
				// one-time only: persistent requests would re-launch after an
				// interruption, which for a finished job is a pure cost bug.
				// Note ValidUntil is NOT supported for one-time requests and
				// BlockDurationMinutes is deprecated, so neither can bound
				// runtime here.
				SpotInstanceType:             ec2types.SpotInstanceTypeOneTime,
				InstanceInterruptionBehavior: ec2types.InstanceInterruptionBehaviorTerminate,
				// MaxPrice deliberately unset: AWS warns it increases
				// interruptions, and the spot price is already <= on-demand.
			},
		}
	}
	return in
}

func (a *AWSProvider) dryRunLaunch(ctx context.Context) error {
	amiID, err := a.resolveAMI(ctx)
	if err != nil {
		return err
	}
	var instType string
	for t := range a.settings.InstanceTypeRates {
		instType = t
		break
	}
	if instType == "" {
		return fmt.Errorf("no instance_type_rates configured")
	}

	in := a.buildRunInput(amiID, LaunchRequest{
		Label:          "kh-preflight",
		IdempotencyKey: "kh-preflight",
		Offer:          Offer{InstanceType: instType},
		DiskGB:         30,
		TTL:            time.Hour,
	}, "#!/bin/bash\ntrue\n")
	in.DryRun = aws.Bool(true)

	_, err = a.ec2.RunInstances(ctx, in)
	if isDryRunSuccess(err) {
		return nil
	}
	return err
}

// isDryRunSuccess reports whether the error is EC2's "you would have been
// allowed" signal.
func isDryRunSuccess(err error) bool {
	if err == nil {
		return false // a dry run should never actually succeed
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "DryRunOperation"
	}
	return false
}

func isUnauthorized(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "UnauthorizedOperation" || apiErr.ErrorCode() == "AccessDenied"
	}
	return false
}

// Launch starts one instance.
func (a *AWSProvider) Launch(ctx context.Context, req LaunchRequest) (*LaunchResult, error) {
	amiID, err := a.resolveAMI(ctx)
	if err != nil {
		return nil, err
	}

	userData, err := BuildCloudInitUserData(req)
	if err != nil {
		return nil, fmt.Errorf("aws: build user data: %w", err)
	}

	out, err := a.ec2.RunInstances(ctx, a.buildRunInput(amiID, req, userData))
	if err != nil {
		if isCapacityError(err) {
			return nil, ErrOfferUnavailable
		}
		return nil, fmt.Errorf("aws: RunInstances: %w", err)
	}
	if len(out.Instances) == 0 {
		return nil, fmt.Errorf("aws: RunInstances returned no instances")
	}

	inst := out.Instances[0]
	launched := time.Now()
	if inst.LaunchTime != nil {
		launched = *inst.LaunchTime
	}
	return &LaunchResult{
		ProviderInstanceID: aws.ToString(inst.InstanceId),
		LaunchedAt:         launched,
		// Billing starts when the instance reaches running; pending is free.
		// LaunchTime is the correct anchor, and is authoritative from AWS
		// rather than our own clock.
		BilledFrom: launched,
		Raw:        models.JSONMap{"instance_id": aws.ToString(inst.InstanceId), "ami": amiID},
	}, nil
}

func isCapacityError(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "InsufficientInstanceCapacity", "VcpuLimitExceeded", "MaxSpotInstanceCountExceeded":
			return true
		}
	}
	return false
}

// Status polls one instance.
func (a *AWSProvider) Status(ctx context.Context, id string) (*InstanceStatus, error) {
	out, err := a.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && strings.Contains(apiErr.ErrorCode(), "NotFound") {
			return &InstanceStatus{ProviderInstanceID: id, State: ObservedGone, Terminal: true}, nil
		}
		return nil, fmt.Errorf("aws: DescribeInstances: %w", err)
	}
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			state, terminal := normalizeAWSState(inst.State)
			return &InstanceStatus{ProviderInstanceID: id, State: state, Terminal: terminal}, nil
		}
	}
	return &InstanceStatus{ProviderInstanceID: id, State: ObservedGone, Terminal: true}, nil
}

func normalizeAWSState(s *ec2types.InstanceState) (InstanceObservedState, bool) {
	if s == nil {
		return ObservedGone, true
	}
	switch s.Name {
	case ec2types.InstanceStateNameRunning:
		return ObservedRunning, false
	case ec2types.InstanceStateNamePending:
		return ObservedPending, false
	case ec2types.InstanceStateNameStopping, ec2types.InstanceStateNameStopped:
		return ObservedStopped, false
	case ec2types.InstanceStateNameShuttingDown, ec2types.InstanceStateNameTerminated:
		return ObservedGone, true
	default:
		return ObservedPending, false
	}
}

// ListOwned returns our instances keyed by label. Filtering on the managed tag
// is what keeps this from ever seeing, or acting on, someone else's instances.
func (a *AWSProvider) ListOwned(ctx context.Context) (map[string]InstanceStatus, error) {
	out, err := a.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:" + TagManaged), Values: []string{"true"}},
			{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws: DescribeInstances: %w", err)
	}

	result := make(map[string]InstanceStatus)
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			var label string
			for _, t := range inst.Tags {
				if aws.ToString(t.Key) == TagLabel {
					label = aws.ToString(t.Value)
					break
				}
			}
			if label == "" {
				continue
			}
			state, terminal := normalizeAWSState(inst.State)
			result[label] = InstanceStatus{
				ProviderInstanceID: aws.ToString(inst.InstanceId),
				State:              state,
				Terminal:           terminal,
			}
		}
	}
	return result, nil
}

// Destroy terminates an instance.
//
// SkipOsShutdown bypasses the graceful OS shutdown: a compute worker has
// nothing to flush, and a hung GPU driver can otherwise leave the instance in
// shutting-down — still billing — for a long time.
func (a *AWSProvider) Destroy(ctx context.Context, id string) error {
	_, err := a.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds:    []string{id},
		SkipOsShutdown: aws.Bool(true),
	})
	if err == nil {
		return nil
	}
	// Already gone is success: the contract requires convergence, and AWS
	// cannot bill for an instance that does not exist.
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.ErrorCode(), "NotFound") {
		return nil
	}
	debug.Error("aws: TerminateInstances failed for %s: %v", id, err)
	return fmt.Errorf("aws: TerminateInstances: %w", err)
}

// CostSoFar has no authoritative answer in real time. Cost Explorer lags up to
// 24 hours, so it can only ever reconcile after the fact — never gate a launch.
func (a *AWSProvider) CostSoFar(ctx context.Context, id string) (int64, bool, error) {
	return 0, false, nil
}
