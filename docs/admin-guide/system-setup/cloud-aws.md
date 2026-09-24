# AWS EC2

Renting GPU capacity from **your own AWS account**.

!!! success "Tested — the only provider driven end to end with real money"
    A real rental was taken through boot, commissioning, cracking, clean release and a settled
    budget refund: 141s to a registered agent, 3/3 hashes cracked, released at 3.8 minutes
    with 54c of the 57c reserved refunded. Every other provider is
    [Experimental](cloud-providers.md#maturity-which-providers-have-actually-been-paid-for).

Your hardware, your IAM, AWS datacenters, SOC 2. No third-party acknowledgement and no
per-job peer-host opt-in.

## Quotas come first

**GPU quotas default to zero on every AWS account.** Until an increase is granted, no GPU
instance can launch at all.

| Quota | Code |
|---|---|
| Running On-Demand G and VT instances (vCPUs) | `L-DB2E81BA` |
| All G and VT Spot Instance Requests (vCPUs) | `L-3819A6DF` |

On-demand and spot quotas are independent. Request increases before first use; a first-ever
GPU request can take days and may be partially granted.

!!! tip "The quota is your only true concurrency ceiling"
    Set it to exactly the vCPUs you are willing to run. No bug in KrakenHashes can exceed it.
    Combine with a dedicated AWS account so nothing else consumes it.

## Spot is often not cheaper

Observed GPU spot discounts run 3–40%, not the advertised "up to 90%". Current-gen parts (L4,
L40S, Blackwell) barely discount because demand is saturated. Compare before enabling.

---

## Account setup

### 1. A dedicated account, or at least a dedicated quota

Orphan reconciliation terminates instances tagged `krakenhashes:managed` that the database
does not recognise. The tag is applied by KrakenHashes and enforced by IAM, so the blast
radius is bounded — but the GPU quota is shared with everything else in the account, so a
dedicated one makes the quota a real ceiling rather than a shared allowance.

### 2. Networking

The worker needs **outbound** connectivity only — it dials your VPN and the provider control
plane. Nothing connects *to* it.

| Resource | Requirement |
|---|---|
| VPC | Any. One subnet per availability zone you want to use |
| Subnet | Must be able to reach the internet — a public subnet with auto-assign public IP, or a private subnet behind a NAT gateway |
| Security group | Outbound to your VPN's endpoint and port. **No inbound rules are needed** |

A NAT gateway costs more per hour than some of the smaller GPU instances, so a public subnet
is usually the cheaper choice for ephemeral workers.

### 3. The provisioner IAM principal

Create a dedicated IAM user (or role) — the reference deployment uses a user named
`krakenhashes-provisioner` — and attach the policy below. Generate an access key for it and
paste the key id and secret into the provider form.

---

## IAM policy

This is the policy the reference deployment actually runs, published as-is. It is what drove
the paid end-to-end launch and the zone picker. Replace `<ACCOUNT_ID>` and `<REGION>`.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "RunInstancesOnResourcesItDoesNotCreate",
      "Effect": "Allow",
      "Action": "ec2:RunInstances",
      "Resource": [
        "arn:aws:ec2:<REGION>::image/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:subnet/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:security-group/*"
      ]
    },
    {
      "Sid": "RunInstancesOnResourcesItCreates",
      "Effect": "Allow",
      "Action": "ec2:RunInstances",
      "Resource": [
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:instance/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:volume/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:network-interface/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:spot-instances-request/*"
      ],
      "Condition": {
        "StringEquals": {
          "aws:RequestTag/krakenhashes:managed": "true"
        }
      }
    },
    {
      "Sid": "TagOnlyAtCreation",
      "Effect": "Allow",
      "Action": "ec2:CreateTags",
      "Resource": [
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:instance/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:volume/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:network-interface/*",
        "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:spot-instances-request/*"
      ],
      "Condition": {
        "StringEquals": {
          "ec2:CreateAction": "RunInstances"
        }
      }
    },
    {
      "Sid": "TerminateOnlyWhatItCreated",
      "Effect": "Allow",
      "Action": "ec2:TerminateInstances",
      "Resource": "arn:aws:ec2:<REGION>:<ACCOUNT_ID>:instance/*",
      "Condition": {
        "StringEquals": {
          "aws:ResourceTag/krakenhashes:managed": "true"
        }
      }
    },
    {
      "Sid": "DescribeIsAllOrNothing",
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceTypes",
        "ec2:DescribeInstanceTypeOfferings",
        "ec2:DescribeImages",
        "ec2:DescribeSubnets",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeVpcs",
        "ec2:DescribeAvailabilityZones",
        "ec2:DescribeSpotPriceHistory",
        "ec2:DescribeSpotInstanceRequests",
        "ec2:DescribeTags",
        "ec2:DescribeVolumes",
        "ec2:GetConsoleOutput",
        "ec2:GetSpotPlacementScores"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ResolveTheDeepLearningAMI",
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter",
        "ssm:GetParameters"
      ],
      "Resource": "arn:aws:ssm:<REGION>::parameter/aws/service/deeplearning/*"
    },
    {
      "Sid": "PreflightQuotaAndIdentity",
      "Effect": "Allow",
      "Action": [
        "servicequotas:GetServiceQuota",
        "servicequotas:GetAWSDefaultServiceQuota",
        "sts:GetCallerIdentity"
      ],
      "Resource": "*"
    }
  ]
}
```

### Why it is shaped that way

Each statement exists because the obvious simplification breaks it.

| Sid | The trap it avoids |
|---|---|
| `RunInstancesOnResourcesItDoesNotCreate` | `ec2:RunInstances` must be allowed on **every** ARN type it touches. Note there is **no `aws:RequestTag` condition here** — `aws:RequestTag` applies only to resources the call *creates*, so putting one on the image, subnet or security-group statement denies everything |
| `RunInstancesOnResourcesItCreates` | The tag condition belongs only on created resources. `spot-instances-request` must be listed or spot launches fail |
| `TagOnlyAtCreation` | A separate `ec2:CreateTags` statement gated on `ec2:CreateAction` is required, or `TagSpecifications` fails the whole request. Gating on the create action means the key cannot re-tag anything afterwards |
| `TerminateOnlyWhatItCreated` | Conditioned on `aws:ResourceTag/krakenhashes:managed`, so the service can never terminate an instance it did not create |
| `DescribeIsAllOrNothing` | `aws:ResourceTag` is **not evaluated for `Describe*`** calls. They cannot be scoped; it is all or nothing |
| `ResolveTheDeepLearningAMI` | Scoped to the public AWS deep-learning parameter path, not `ssm:*` |
| `PreflightQuotaAndIdentity` | Read-only, used only by pre-flight |

!!! warning "Missing one ARN type gives you no hint which"
    A `RunInstances` call denied on any single resource type returns a bare
    `UnauthorizedOperation`. That is why the two `RunInstances` statements enumerate every
    type rather than relying on a wildcard.

### What is required and what merely degrades

Not every action is load-bearing. Tightening the policy costs information rather than
function in several places, and knowing which is which saves a debugging session.

| Action | Status |
|---|---|
| `ec2:RunInstances`, `ec2:CreateTags`, `ec2:TerminateInstances` | **Required.** Launch and teardown |
| `ec2:DescribeInstances` | **Required.** Quota usage, per-instance state polling, and the reaper's orphan sweep |
| `ec2:DescribeAvailabilityZones`, `ec2:DescribeSubnets` | **Required for the zone picker.** Without them the capacity screen returns an error instead of rendering |
| `ssm:GetParameter` | Required **if** you use `ami_ssm_parameter` |
| `ec2:DescribeInstanceTypeOfferings` | Degrades. Every cell renders as "offered" with a warning, so you lose the one definitive availability signal |
| `ec2:DescribeSpotPriceHistory` | Degrades. The grid shows your configured rates only |
| `ec2:GetSpotPlacementScores` | Degrades. Costs launch **ordering** and nothing else — it never removes a candidate. The warning names the action |
| `servicequotas:*`, `sts:GetCallerIdentity` | Pre-flight only. See the caveat below |
| `ec2:GetConsoleOutput` | **Not used by KrakenHashes at all.** Included so a human can read a failed instance's boot log from the console |

!!! warning "Pre-flight is stricter than launching"
    Pre-flight treats "inconclusive" as failure, and a denied `servicequotas:*` or
    `ec2:DescribeInstances` lands there. So a policy tight enough to omit them shows a **red
    pre-flight on a configuration that would launch perfectly well**. Grant them, or read the
    report rather than the headline.

!!! note "`iam:PassRole` is only needed if you set an instance profile"
    The policy above has no `iam:PassRole` statement and the reference deployment has no
    worker instance profile, because the agent does not call AWS APIs. If you set
    `iam_instance_profile_arn`, add `iam:PassRole` scoped to that exact role ARN with
    `iam:PassedToService = ec2.amazonaws.com` — unscoped, it is a privilege-escalation vector.

---

## Credentials

Supplied as a JSON document in the credentials field. The admin form builds this for you from
two inputs.

```json
{
  "access_key_id": "AKIA...",
  "secret_access_key": "..."
}
```

`session_token` is also accepted for STS temporary credentials, but **has no field in the
admin form** — a config using it has to be POSTed to the API directly.

Credentials are AES-256-GCM encrypted at rest and never returned by the API; the config
exposes only a `has_credentials` boolean. Leaving the field blank on an edit keeps what is
stored, so changing a rate ceiling cannot silently blank your key.

---

## Settings

```json
{
  "region": "us-east-1",
  "security_group_ids": ["sg-..."],
  "ami_ssm_parameter": "/aws/service/deeplearning/ami/x86_64/base-oss-nvidia-driver-gpu-ubuntu-22.04/latest/ami-id",
  "use_spot": false,
  "instance_type_rates": { "g4dn.xlarge": 53, "g6e.xlarge": 187 },
  "instance_type_gpus": {
    "g4dn.xlarge": { "gpu_model": "T4",   "gpu_count": 1, "vram_gb_per_gpu": 16 },
    "g6e.xlarge":  { "gpu_model": "L40S", "gpu_count": 1, "vram_gb_per_gpu": 48 }
  },
  "root_volume_gb": 100,
  "ebs_cents_per_gb_month": 8,
  "zones": [
    { "zone": "us-east-1a", "zone_id": "use1-az1", "subnet_id": "subnet-...", "instance_types": [] },
    { "zone": "us-east-1b", "zone_id": "use1-az2", "subnet_id": "subnet-...", "instance_types": ["g4dn.xlarge"] }
  ]
}
```

| Field | Required | Notes |
|---|---|---|
| `region` | **Yes** | Everything else must live in it |
| `ami_id` *or* `ami_ssm_parameter` | **One of them** | Prefer the SSM parameter so image updates are picked up without a config change |
| `instance_type_rates` | **Yes** | Cents per hour. Doubles as the instance-type allowlist — a type with no rate is never offered |
| `instance_type_gpus` | Strongly recommended | See below |
| `security_group_ids` | In the form, yes | Omitted from the launch when empty, in which case EC2 applies the VPC's **default** security group. Set it explicitly |
| `zones` | Strongly recommended | See [Zones](#zones-why-one-subnet-is-not-enough) |
| `use_spot` | No | Default `false` |
| `root_volume_gb` | No | A floor; the job's fileset may raise it. The launch additionally floors at 30 GB |
| `ebs_cents_per_gb_month` | No | Default `8.0` (us-east-1 gp3). The storage term of the reservation |
| `iam_instance_profile_arn` | No | Only if the agent itself needs to call AWS APIs |

`instance_type_rates` is in **cents per hour** and is operator-supplied rather than resolved
from the AWS Pricing API: that API needs exact `capacitystatus`/`preInstalledSw`/`tenancy`
filters or it silently returns the wrong SKU.

!!! warning "Omitting `instance_type_gpus` quietly disables cost-per-work ranking"
    Without it an offer falls back to the **instance type** as its GPU model name, which
    matches no known GPU class. The ranker then rates it as unknown hardware and the ordering
    collapses to a monotone function of price per hour — exactly what cost-per-work exists to
    replace. The cheapest box wins even when it is the worst value.

    Declaring `gpu_count` matters as much as the model: treating a 4-GPU `g5.12xlarge` as
    having one GPU makes it look four times worse per dollar than it is.

    It also affects filtering — a VRAM floor or a GPU-model allowlist will drop AWS offers
    wholesale if the hardware was never declared.

---

## Zones: why one subnet is not enough

**EC2 spot capacity is a property of the (instance type, availability zone) pair.**
`g4dn.xlarge` being exhausted in `us-east-2b` says nothing about `g4dn.xlarge` in
`us-east-2c` — they are different physical inventories. So the number of independent chances
a launch gets is:

```
capacity pools = (zones you allow) x (instance types you allow in them)
```

A config with one `subnet_id` and one instance type has **exactly one pool**. When it is
empty, provisioning simply stops: the launch retry loop walks its candidate list, but every
candidate resolves to the same inventory, so the second refusal was implied by the first.
Measured on a real deployment: fifteen consecutive `InsufficientInstanceCapacity` refusals
against one pool, then a success within two scheduler cycles of widening to three instance
types across two zones. Nothing about the account, quota or region changed.

Each entry in `zones` is one placement. `subnet_id` is the only required field — a launch
takes a subnet, not a zone. An **empty `instance_types` means "every type in
`instance_type_rates`"**, which is what you want by default and what keeps the selection from
silently narrowing when you price a new instance type later. Narrow it per zone only when you
know a particular card is never obtainable there.

`zone_id` (`use2-az1`) is the stable physical identifier; `zone` (`us-east-2a`) is an alias
AWS shuffles **per account**, so your `us-east-2a` and another account's are usually different
datacentres. Capacity APIs report the zone ID, which is why both are stored.

!!! note "The Subnet ID field was removed from the settings form"
    It expressed the same thing as the zone picker — placement — and the backend ignores it
    the moment any zone is selected, so having both on screen invited editing dead config.

    Nothing breaks. A stored `subnet_id` is preserved and still honoured while no zones are
    configured. To migrate an existing config: open the provider, click **Check availability**,
    and your current subnet's zone is already ticked and labelled *"selected via the legacy
    single subnet_id setting"*. Saving converts it. The field is still accepted by the API for
    automation.

When `zones` is set, the top-level `subnet_id` is ignored. It remains supported on its own for
existing configs and as the fallback when no zones are selected.

## Picking zones from the UI

**Admin → Settings → Cloud Provisioning → Providers → (edit an AWS provider) → Availability
zones and instance types.**

"Check availability" (`GET /api/admin/cloud/providers/{id}/capacity`) reads live data and
**spends nothing** — it is read-only EC2 describe calls, with the spot price and placement-score
lookups made only when `use_spot` is set. It renders a zone × instance-type grid where each
ticked cell is one capacity pool, with the running pool count underneath.

Read the columns in this order of trustworthiness:

| Signal | Source | How much to trust it |
|---|---|---|
| **Offered / not offered** | `DescribeInstanceTypeOfferings` | **Definitive.** A type not offered in a zone can never launch there |
| **Free IPs, subnet, zone state** | `DescribeSubnets`, `DescribeAvailabilityZones` | Definitive. A zone with no subnet or no free addresses cannot launch |
| **Spot price** | `DescribeSpotPriceHistory` | Real, but a price is not an inventory |
| **Placement score (n/10)** | `GetSpotPlacementScores` | **Weak.** See below |

The placement score is AWS's own relative ranking of a pool, and it is worth less than it
looks: every `us-east-2` zone scored **1/10** on a live account minutes before a `g4dn.xlarge`
launch in one of them succeeded on the first attempt. It is used to decide which pool to try
**first** and for nothing else — it never removes a pool from the candidate list, and gating on
it would have refused a launch that worked. If your IAM role lacks
`ec2:GetSpotPlacementScores`, you lose the ordering and nothing else; the screen says so rather
than failing.

!!! info "Scores are fetched per instance type, not for the set"
    `GetSpotPlacementScores` scores the **set** of instance types you pass it as a whole. The
    picker therefore makes one call per type, because a single call for the set would paint the
    same number into every cell — hiding that one card scores 1/10 in a zone where the set
    scores 9/10.

The **spot price is shown next to your configured rate** because the two drift and only one is
real. Reservations are denominated in your configured figure, so pricing above the live rate
merely over-reserves; pricing **below** it under-reserves against a cap you were told was
hard. One deployment had `g4dn.xlarge` priced at 53c while spot was quoting 27c.

The screen reads the **saved** provider config, so save before checking: an unsaved form's
credentials and region are not visible to the backend.

---

## Verifying

Run the pre-flight from the provider page. For AWS it reports, separately:

- whether credentials resolve (`sts:GetCallerIdentity`)
- the applied or default quota, and current usage
- exactly which IAM permissions are missing, via a real `DryRun`

Anything inconclusive is treated as **failure**, not success: "unknown" is not "allowed".

!!! warning "DryRun does not check quota or capacity"
    It validates IAM and the exact parameter set, against a real placement from your zone
    list. A launch can still fail afterwards with `VcpuLimitExceeded` or
    `InsufficientInstanceCapacity` — the first is a quota that will never clear without a
    limit-increase request, the second is transient.

Then rent one cheap instance with a short TTL and confirm teardown. See
[Verifying before you spend](cloud-providers.md#verifying-before-you-spend) for the $0
rehearsal that should come first.

## Related

- [Cloud GPU Providers](cloud-providers.md) — budgets, provisioning rules, trust tiers
- [Cloud Agent VPN](cloud-vpn.md) — the VPN credential every provider needs
- [Cloud GPU Provisioning](../../reference/architecture/cloud-provisioning.md) — design rationale, spot request details
