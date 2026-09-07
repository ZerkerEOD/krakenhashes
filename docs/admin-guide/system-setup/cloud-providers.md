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
subject alternative names.

Add it in **Admin → Settings → Server Certificate** and click **Apply & Reissue** — for
example the Tailscale name `kraken.tailnet-xxxx.ts.net` and its CGNAT address
`100.113.129.115`. CGNAT addresses (`100.64.0.0/10`) are permitted out of the box, since
that is what Tailscale uses and what NetBird's default account network is drawn from.

!!! warning "Self-hosted NetBird may sit outside CGNAT"
    A self-hosted NetBird can be configured with any network range, and it is easy to
    pick one that is not private. `100.133.64.0/19` looks like CGNAT because it starts
    with `100.`, but CGNAT stops at `100.127.255.255`. KrakenHashes refuses addresses
    above that, with no override.

    Check with `netbird status` (the `NetBird IP` line) or `ip -o -4 addr show wt0`.
    If your range is outside `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` or
    `100.64.0.0/10`, change the VPN's network range. Renumbering re-allocates every
    peer, so update `BackendVPNHost` and the certificate SAN list afterwards.

The reissue is immediate and non-disruptive: it uses the existing certificate authority, so
enrolled agents are unaffected and the backend needs no restart. You do **not** need to set
this before first boot, and you must not delete the certs directory to change it.

!!! note "Enabling a provider only checks that a VPN host is configured"
    KrakenHashes requires `BackendVPNHost` to be non-empty before a provider can be
    enabled, but it does not currently verify that the address appears in the
    certificate. Check it yourself after adding the provider:

    ```bash
    openssl s_client -connect <vpn-address>:31337 </dev/null 2>/dev/null \
      | openssl x509 -noout -text | grep -A1 "Subject Alternative Name"
    ```

    If a rented instance cannot connect, its reported address also appears under
    **Discovered addresses** on the Server Certificate page.

---

## Trust tiers: which providers carry a warning, and why

The consent machinery attaches to **third-party hardware**, not to "is it cloud". Two of the
five provider kinds are third-party; three are not.

| Kind | Hardware | Compliance | Consent chain |
|---|---|---|---|
| `aws` | Your **own** AWS account, AWS datacenters, your IAM | SOC 2 | **None** |
| `runpod` (Secure Cloud) | RunPod's **own** datacenters, single-tenant per host | SOC 2 Type II, ISO 27001, PCI DSS | **None** |
| `runpod_community` | **Peer-operated** machines; the owner has root over the container | **None of RunPod's attestations cover this tier** | **Full** |
| `vastai` | **Individually-owned** consumer machines; the owner has root over the container | None | **Full** |
| `mock` | Local `agent --test-mode` processes | n/a | None |

!!! danger "What the peer tiers actually expose"
    On `vastai` and `runpod_community` the machine's owner has **root over the container**.
    Hashes, wordlists, potfiles and cracked plaintexts placed there are readable by a third
    party and are **not encrypted at rest on the host**. The protection is a terms-of-service
    clause, not an isolation boundary. Do not use these tiers for production or client
    engagement data.

    Those two kinds require **three** separate opt-ins, each attributed in the audit log:

    1. A provider-level acknowledgement before the config can be enabled.
    2. A per-client acknowledgement before that client may allowlist it.
    3. A **per-job** opt-in — *Allow peer-operated hosts* — before any job lands on one.

    Without the per-job flag a job simply does not see peer offers. It may still rent secure
    capacity from the rest of the client's allowlist, so leaving it off degrades rather than
    blocks.

!!! note "AWS and RunPod Secure enable in one step"
    Neither carries an acknowledgement dialog, a red chip, or the per-job flag. That is
    deliberate: gating hardware you already control behind a data-exposure warning teaches
    operators the warning is noise, and the one place it is real stops being read.

Allowing AWS while forbidding the peer tiers is a first-class configuration —
`cloud_provider_allowlist` is **empty by default**.

---

## Vast.ai

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

## RunPod

RunPod is configured as **two separate provider kinds**, not one with a tier setting:

- **`runpod` — Secure Cloud.** RunPod's own datacenters, single-tenant per host, covered by
  their SOC 2 Type II, ISO 27001 and PCI DSS attestations. Treat it like AWS.
- **`runpod_community` — Community Cloud.** Peer-operated machines. See the trust-tier table
  above; none of those attestations extend to this tier.

Splitting them is what lets a client allowlist Secure without ever being exposed to
Community, and what keeps the consent chain attached to the tier that needs it.

### Account setup

1. Use a **dedicated RunPod account**. Ownership is determined client-side by matching pod
   names against KrakenHashes' `kh-` label shape, because the v2 API offers no idempotency
   key, no server-side filter and no way to tag a pod as ours. Orphan reconciliation will
   destroy unrecognised pods that match that shape.
2. Fund it with a **fixed prepaid balance and no auto-refill**. The v2 API exposes no balance
   endpoint at all, so pre-flight cannot verify funding — the only signal is a `402` at
   create time.
3. Create an API key with pod read/write.

### Operational caveats

- **No provider-enforced TTL.** RunPod has no `autoTerminate` or `expiresAt` field, so
  teardown rests on the in-guest deadline and the backend reaper. Prefer shorter TTLs here
  than you would on AWS.
- **A stopped pod still bills**, with volume disk charged at roughly double the running rate.
  KrakenHashes always terminates and never stops, and never attaches network volumes — those
  outlive the pod and would hold cracked plaintexts after termination.
- **Billing granularity is one hour**, so cost-so-far reads as unknown for any pod that lived
  less than that. Accrual stays wall-clock based.

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

## Provisioning rules: when the system may spend

Budgets govern **how much** may be spent. Provisioning rules govern **when anything may be
spent at all** — the rails you need before leaving the autoscaler unattended. Without them
it rents after a single starving tick, for any job, at any hour, with no per-job ceiling.

Rules live at two levels. The **system default** applies everywhere; a **per-client override**
layers on top **field by field**, so an unset field inherits. That differs from budget
policies, which pick one whole row — and the difference matters in the direction that costs
money: tightening a default must reach the clients that already have an override, because
those are exactly the ones most likely to need tightening.

Every rule has an in-band "off" value so a client can switch off an inherited rule without a
second toggle per rule.

| Rule | Default | Off value | Applies to |
|---|---|---|---|
| **Minimum job priority** | `0` (off) | `0` | Autoscaler only |
| **Minimum starvation time** | `180s` | `0` | Autoscaler only |
| **Skip if finishing within** | `900s` | `0` | Autoscaler only |
| **Maximum spend per job** | `0` (off) | `0` | **Everything, including admins** |
| **Provisioning window** | none | start = end | **Everything, including admins** |

### Why two of them are not admin-bypassable

The first three mean *"not important enough to spend on **automatically**"*. An operator
clicking **Provision** has already made that judgement by hand, so blocking them would turn
an autoscaler tuning knob into a lockout with no override.

The last two are different in kind:

- **A per-job spend cap an admin can click past is not a spend cap.**
- **A provisioning window usually encodes something external** — a contract clause, a client's
  change freeze, a maintenance period — not an operator preference. "I am an admin" is not the
  authority that overrides someone else's policy.

Both **fail closed**: if the committed spend for a job cannot be read, or the window's timezone
cannot be resolved, provisioning refuses. A ceiling you cannot read has to behave like one
that is engaged.

### Notes on individual rules

**Minimum job priority** is an **absolute** `job_executions.priority` value, not a percentage
of your ceiling. See [Job Priority](../advanced/job-priority.md) — a floor of `700` means
"High and above" at the default ceiling of 1000 and matches *nothing* on a 0-100 deployment.
The admin UI renders the floor against your live ceiling and shows how many queued jobs
currently clear it; a count of zero is displayed as a warning.

**Minimum starvation time** changes shipped behaviour. Before this rule, one starving
scheduler tick was enough, so a transient gap between chunks could cost a full instance
launch. The 180-second default is three publish intervals. A job counts as starving only while
it makes **no progress at all** — a job that receives any allocation has its clock reset,
even if other units of the same job went unserved.

**Skip if finishing within** avoids renting for a job that will finish before the instance
finishes booting and syncing files (roughly 5–10 minutes). It fires only on a projection the
estimator actually trusts: a job with no throughput reports a duration of zero, and that means
*unknown*, never *instant*. Projections for salted hash types are deliberately pessimistic, so
overriding this per client is legitimate.

**Maximum spend per job** covers one job execution over its **whole life** and is deliberately
not month-windowed — a per-job cap that resets at a month boundary is not a per-job cap. The
cost of the launch under consideration counts against it *before* the launch happens.

**Provisioning window** governs **starts only**. An instance is never torn down because the
window closed; TTL and idle drain own teardown, and killing a mid-chunk instance would waste
everything already paid for it. `end` earlier than `start` **wraps midnight**, which is the
normal way to write "only rent overnight". Times are evaluated in the configured IANA zone, so
a UTC server can still express local business hours.

---

## Opting a job in

Nothing bursts to paid capacity by accident. The opt-in exists in three places, all off by
default:

| Where | Field | Notes |
|---|---|---|
| Preset job | *Allow cloud burst* + *Max cloud instances* + *Allow peer-operated hosts* | Copied onto every job created from the preset |
| Workflow | *Allow cloud burst for every step* | Overrides each step's burst setting. **Does not** grant peer consent — that stays with each preset |
| Custom job dialog | *Allow Cloud Burst* + *Max Cloud Instances* + *Allow peer-operated hosts* | One job only |

The workflow toggle deliberately governs bursting only. "Spend money" and "put this client's
hashes on someone else's machine" are separate decisions, and a workflow-level switch that
silently granted the second would make the per-job consent meaningless.

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
