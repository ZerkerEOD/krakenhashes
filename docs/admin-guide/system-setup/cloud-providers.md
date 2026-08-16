# Cloud GPU Providers

Configure Vast.ai or AWS EC2 so KrakenHashes can rent GPU capacity on demand.

!!! warning "Nothing bursts by accident"
    Cloud provisioning requires **three** independent opt-ins: the provider must be
    enabled, the client must have a funded budget and allow that provider, and the job
    (or its preset/workflow) must be marked cloud-eligible. Missing any one means no
    instance is ever rented.

## Prerequisites

### 1. An encryption key

Provider credentials and VPN enrollment credentials are AES-256-GCM encrypted at rest.
Set `KH_ENCRYPTION_KEY` before configuring anything:

```bash
openssl rand -base64 32
```

Without it the server generates an ephemeral key and every secret written by that process
becomes unrecoverable on restart. See [Environment](../../reference/environment.md).

### 2. A VPN the agents can join

The KrakenHashes server should not be exposed to the internet. Cloud agents reach it over
your **existing** VPN. Supported: **Tailscale, NetBird, WireGuard**.

!!! danger "OpenVPN is not supported"
    Vast.ai runs unprivileged containers with no `/dev/net/tun` and no `NET_ADMIN`, and
    its API silently discards `--cap-add`/`--device`. OpenVPN cannot work there, and the
    only "userspace" path for it is unmaintained out-of-tree patches to a
    security-critical binary.

| Provider | Per-instance credential | Auto-deregistration |
|---|---|---|
| **Tailscale** (recommended) | OAuth client → one-off ephemeral tagged key per instance | 30–60 min, or instant on logout |
| **NetBird** | PAT → one-off ephemeral setup key per instance | ~10 min |
| **WireGuard** | none — static operator config | none, manual |

Prefer the OAuth/PAT path. A reusable Tailscale auth key is capped at **90 days**, so it is
a scheduled outage; KrakenHashes tracks its expiry, warns as it approaches, and **refuses
to provision once it lapses** rather than launching an instance that can never connect.

!!! info "NetBird has no DNS in netstack mode"
    NetBird's userspace mode provides no DNS. Pin the backend's overlay IP as the cloud
    host and make sure the server certificate has a matching IP SAN.

### 3. The server certificate must cover the VPN address

Cloud agents connect to the backend at its **VPN** address, and the agent sets no explicit
`ServerName`, so SNI is whatever host it dials. That address must be in the certificate's
SANs **before** the certificate is generated:

```bash
KH_ADDITIONAL_DNS_NAMES=localhost,kraken.internal,kraken.tailnet-xxxx.ts.net
KH_ADDITIONAL_IP_ADDRESSES=127.0.0.1,100.113.129.115
```

KrakenHashes validates this when you enable a provider and refuses with an actionable
message if the configured host is not covered.

---

## Vast.ai

!!! danger "Third-party data exposure"
    Vast.ai rents GPUs on **individually-owned machines whose operators have root over the
    container**. Hashes, wordlists, potfiles and cracked plaintexts placed there are
    exposed to a third party. This is categorically different from AWS, where the instance
    runs inside *your own* account.

    Enabling Vast.ai requires an explicit acknowledgement, and each client must be opted in
    separately. Both are recorded in the audit log with attribution.

    Allowing AWS while forbidding Vast.ai is a first-class configuration —
    `cloud_provider_allowlist` is **empty by default**.

### Account setup

1. Use a **dedicated Vast.ai account**. Orphan reconciliation destroys anything on the
   account that KrakenHashes does not recognise.
2. Fund it with a **fixed prepaid balance and no auto-billing card**. Vast.ai offers no
   spending cap; a prepaid balance is the only hard ceiling available.
3. Create a scoped API key with `misc`, `user_read`, `instance_read`, `instance_write`.
   Offer search lives under `misc`, not `instance_read` — this is easy to get wrong.

Only **verified datacenter** hosts are ever offered, unconditionally. The cheaper
unverified tier is also where "stuck connecting" and "bad driver" reports concentrate.

---

## AWS

### Quotas come first

**GPU quotas default to zero on every AWS account.** Until an increase is granted, no GPU
instance can launch at all.

| Quota | Code |
|---|---|
| Running On-Demand G and VT instances (vCPUs) | `L-DB2E81BA` |
| All G and VT Spot Instance Requests (vCPUs) | `L-3819A6DF` |

On-demand and spot quotas are independent. Request increases before first use; a first-ever
GPU request can take days and may be partially granted.

!!! tip "The quota is your only true concurrency ceiling"
    Set it to exactly the vCPUs you are willing to run. No bug in KrakenHashes can exceed
    it. Combine with a dedicated AWS account so nothing else consumes it.

### Spot is often not cheaper

Observed GPU spot discounts run 3–40%, not the advertised "up to 90%". Current-gen parts
(L4, L40S, Blackwell) barely discount because demand is saturated. Compare before enabling.

### IAM

The provisioner needs tag-scoped permissions. Key points:

- `ec2:RunInstances` must be allowed on **every** ARN type it touches — `image`, `subnet`,
  `security-group`, `instance`, `volume`, `network-interface`, and `spot-instances-request`
  for spot. Missing one yields a bare `UnauthorizedOperation` with no hint which.
- `aws:RequestTag` conditions apply only to resources the call **creates**. Putting one on
  the `subnet` or `image` statement denies everything.
- A separate `ec2:CreateTags` statement gated on `ec2:CreateAction` is required, or
  `TagSpecifications` fails the request.
- `ec2:TerminateInstances` conditioned on `aws:ResourceTag/krakenhashes:managed` so the
  service can never terminate anything it did not create.
- `iam:PassRole` scoped to the exact worker role ARN with
  `iam:PassedToService=ec2.amazonaws.com`, or this policy is a privilege-escalation vector.
- `aws:ResourceTag` is **not** evaluated for `Describe*` — those are all-or-nothing.

### Settings

```json
{
  "region": "us-east-1",
  "subnet_id": "subnet-...",
  "security_group_ids": ["sg-..."],
  "iam_instance_profile_arn": "arn:aws:iam::123456789012:instance-profile/KrakenHashesWorker",
  "ami_ssm_parameter": "/aws/service/deeplearning/ami/x86_64/base-oss-nvidia-driver-gpu-ubuntu-22.04/latest/ami-id",
  "use_spot": false,
  "instance_type_rates": { "g4dn.xlarge": 53, "g6e.xlarge": 187 },
  "root_volume_gb": 100
}
```

`instance_type_rates` is in **cents per hour** and is operator-supplied rather than resolved
from the AWS Pricing API: that API needs exact `capacitystatus`/`preInstalledSw`/`tenancy`
filters or it silently returns the wrong SKU.

Use `ami_ssm_parameter` rather than pinning `ami_id`, so image updates are picked up
without a config change.

---

## Where the settings live

**Admin → Settings → Cloud Provisioning**, which has three sections:

| Section | What it configures |
|---|---|
| **Providers** | Credentials, VPN, per-provider instance and hourly-rate ceilings, pre-flight |
| **Client budgets** | Per-client spend cap, TTL ceiling, provider allowlist, acknowledgements |
| **Threshold policy** | The system-default notify / stop / drain / hard-stop ladder |

The live fleet is a separate page — **Admin → Cloud Fleet** — because it polls and needs
the full width. It lists every instance that may still be billing, with its state, rate,
spend so far and remaining TTL, plus a manual destroy button. An instance whose teardown
is failing is called out at the top of that page: it is still accruing cost and still
blocking new provisioning for its client until it is dealt with.

Credentials are write-only. The API returns `has_credentials` / `has_vpn_credential`
booleans, never the value, and an edit that leaves a secret field blank keeps what is
already stored — so changing a rate ceiling cannot silently blank an API key.

## Budgets

Per client:

| Field | Meaning |
|---|---|
| `cloud_budget_cents` | Spend ceiling for the current month. Unset ⇒ cloud burst unfunded. |
| `cloud_provider_allowlist` | Which providers this client may use. **Empty by default.** |
| `max_instance_ttl_minutes` | Ceiling on any single instance's life. |

The threshold ladder (notify / stop-provisioning / drain / hard-stop) defaults to
80 / 95 / 99 / 100 and is overridable per client. With `allow_overage=false` the cap is
absolute — see [the architecture doc](../../reference/architecture/cloud-provisioning.md)
for why reservation accounting, not the ladder, is what enforces it.

Vast.ai requires the acknowledgement **twice**: once when enabling the provider, and again
per client before that client may be allowlisted for it. The provider-level acknowledgement
says the operator understands where Vast.ai runs; the per-client one says this particular
engagement's data may go there. Both are attributed to the admin who accepted them.

## Opting a job in

Nothing bursts to paid capacity by accident. The opt-in exists in three places, all off by
default:

| Where | Field | Notes |
|---|---|---|
| Preset job | *Allow cloud burst* + *Max cloud instances* | Copied onto every job created from the preset |
| Workflow | *Allow cloud burst for every step* | Overrides each step's preset setting |
| Custom job dialog | *Allow Cloud Burst* + *Max Cloud Instances* | One job only |

**Max cloud instances is deliberately separate from Max Agents.** Max Agents governs the
shared on-prem pool, where its job is fleet fairness — stopping one job from monopolising
hardware everyone shares. A rented instance is dedicated to one job and paid for by that
job's client, so counting it against that budget would mean a job with the default
`max_agents=1` and one on-prem agent could never use a rented instance it had already paid
for. Leave the cloud cap blank to let the remaining budget decide.

A job with cloud burst enabled shows a **Will this finish within budget?** projection on
its detail page: the coverage bar is the share of the remaining work the budget can pay to
complete. Below 100% the budget runs out before the job does, and launching requires an
explicit acknowledgement — partial progress may still be worth buying, but not by accident.

## Verifying before you spend

Run the pre-flight from the provider page. For AWS it reports, separately:

- whether credentials resolve (`sts:GetCallerIdentity`)
- the applied or default quota, and current usage
- exactly which IAM permissions are missing, via a real `DryRun`

Anything inconclusive is treated as **failure**, not success: "unknown" is not "allowed".

Then rent one cheap instance with a short TTL and confirm teardown by killing the backend
immediately after launch — the in-guest watchdog should still destroy it.
