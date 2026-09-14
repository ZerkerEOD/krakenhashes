# Cloud GPU Providers

Configure AWS EC2, RunPod or Vast.ai so KrakenHashes can rent GPU capacity on demand.

This page covers what every provider shares — prerequisites, trust tiers, budgets, and the
rules governing when anything may be spent. Per-provider setup lives on its own page:
[AWS](cloud-aws.md), [RunPod](cloud-runpod.md), [Vast.ai](cloud-vastai.md), and the
[VPN](cloud-vpn.md) they all require.

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

A VPN is **mandatory** — there is no direct-connection mode, and provisioning fails closed
without a credential, before any provider API call is made. OpenVPN cannot work in an
unprivileged container and is not supported.

### 3. The server certificate must cover the VPN address

Cloud agents connect at the backend's VPN address and set no explicit `ServerName`, so that
address must appear in the server certificate's subject alternative names.

!!! tip "Both prerequisites are covered in one place"
    **→ [Cloud Agent VPN](cloud-vpn.md)** — choosing a VPN, obtaining a credential for each
    of the three, the certificate SAN requirement, why OpenVPN is excluded, and what to check
    when agents launch but never register.

---

## Maturity: which providers have actually been paid for

**Beta does not mean unfinished.** It means nobody has yet driven that adapter end to end
against the real provider, spending real money, and watched it come back clean. An
implemented adapter and a proven one look identical from the outside and cost very
differently when they are wrong.

| Kind | Maturity | What that is based on |
|---|---|---|
| `aws` | **Stable** | Driven end to end on a real account: 141s commissioning, 3/3 hashes cracked, released at 3.8 minutes, 54c of 57c reserved refunded |
| `mock` | **Stable** | Rents nothing and spends nothing |
| `vastai` | **Beta** | Fully implemented; never once paid for. The rental lifecycle is unproven against the live marketplace |
| `runpod` (Secure Cloud) | **Beta** | Written against the documented API with no account to verify it on |
| `runpod_community` | **Beta** | As above, **and teardown is reaper-only** — see [RunPod](cloud-runpod.md#teardown-differs-sharply-between-the-two-tiers) |

!!! warning "What to do when running a beta provider"
    The UI marks these with a **Beta** chip in Provider settings and a banner on the Cloud
    Fleet page whenever a beta instance is live. Expect bugs, and:

    - **Start with a small `cloud_budget_cents` and a low `max_concurrent_instances`.**
      The cheapest bug to survive is one that can only rent one box.
    - **Watch instances reach `terminated` rather than assuming they will.** Teardown is
      the least-tested path in any new adapter and the only one that costs money when it
      fails. The Cloud Fleet page calls out instances whose teardown is failing.
    - **Set a `max_instance_ttl_minutes` you are willing to pay in full**, since it is the
      backstop when every other release path misses.

This is a **separate axis from trust**, and the two only partly overlap: RunPod Secure is
beta but first-party, Vast.ai is both beta and third-party, and `mock` is neither.

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

## Providers

Each provider has its own setup page. All three need a VPN credential and a backend VPN host
first — see [Cloud Agent VPN](cloud-vpn.md).

### [AWS EC2 →](cloud-aws.md)

Your own AWS account, your IAM, AWS datacenters. **Stable** — the only provider driven end to
end with real money. Needs a GPU quota increase (quotas default to **zero** on every account),
an IAM policy, and a set of availability zones. That page publishes the IAM policy the
reference deployment actually runs.

### [RunPod →](cloud-runpod.md)

Two separate provider kinds: **Secure Cloud** (RunPod's own datacenters, single-tenant,
SOC 2 — treat it like AWS) and **Community Cloud** (peer-operated, full consent chain). Both
**beta**. The difference that matters is teardown: a Community pod has no in-guest rail and
the backend reaper is the only thing that can stop it billing.

### [Vast.ai →](cloud-vastai.md)

A marketplace of individually-owned machines. **Beta**, and the host operator has root over
your container. Four selection axes — country, GPU model allow/deny, verified-datacenter
toggle, and a reliability floor.

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
| `max_instance_ttl_minutes` | Ceiling on any single instance's life. A **ceiling, not a target** — see below. |

`max_instance_ttl_minutes` is an upper bound, not the value every instance gets. The
system sizes each rental to what the job actually has left plus its commissioning cost,
so a short job does not hold a four-hour reservation. Setting this **too low costs real
money**: every rented instance pays roughly 20 minutes of boot, sync and benchmark before
it can do any work, so a 30-minute ceiling on a multi-hour job re-pays that setup on every
rental. Setting it generously is close to free, because the unused remainder is refunded
at teardown and an instance that runs out of work is destroyed by idle drain well before
its TTL. **Prefer an hour or more.**

Rentals below the minimum useful length are refused outright rather than made — the
message names both the length on offer and the minimum, and the job records a
`cloud_rental_too_short` diagnostic. That minimum comes from
`cloud_max_commissioning_pct` (see the settings table below).

The threshold ladder (notify / stop-provisioning / drain / hard-stop) defaults to
80 / 95 / 99 / 100 and is overridable per client. With `allow_overage=false` the cap is
absolute — see [the architecture doc](../../reference/architecture/cloud-provisioning.md)
for why reservation accounting, not the ladder, is what enforces it.

The peer-operated tiers — **Vast.ai** and **RunPod Community** — require the acknowledgement
**twice**: once when enabling the provider, and again per client before that client may be
allowlisted for it. The provider-level acknowledgement says the operator understands where
that provider runs; the per-client one says this particular engagement's data may go there.
Both are attributed to the admin who accepted them. AWS and RunPod Secure need neither.

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

## Cloud-only deployments (no on-prem GPUs)

KrakenHashes works with **no GPU hardware of your own** — every agent rented, run, and
destroyed on demand. The feature is named "cloud burst" because it was built to extend an
existing fleet, but nothing in the scheduler requires one: the autoscaler's
"don't rent while an on-prem agent is idle" check simply never engages when there are no
on-prem agents.

Two things are worth understanding before you rely on it.

### Everything is off until you turn it on

The shipped defaults are deliberately fail-closed, and several of them fail **silently** —
the job simply sits at `pending`. Work through this list in order:

| Setting | Default | Where |
|---|---|---|
| `cloud_global_monthly_cap_cents` | **`0` — disables cloud entirely** | Cloud Provisioning → System |
| `cloud_default_client_budget_cents` | **unset — clients stay unfunded** | Cloud Provisioning → Client Budgets |
| `cloud_default_cloud_enabled` | **`false`** | Cloud Provisioning → Client Budgets |
| `cloud_default_provider_allowlist` | **unset** | Cloud Provisioning → Client Budgets |
| `cloud_agent_image` | `…:latest`, which does not exist pre-release | Cloud Provisioning → System |
| `require_client_for_hashlist` | `false` | System Settings |
| `cloud_burst_enabled` | `false`, per job | each preset, workflow, or job |
| `cloud_default_burst_enabled` | **`false`** | Cloud Provisioning → System |
| `cloud_default_client_id` | **unset** | Cloud Provisioning → System |

!!! danger "Work with no client needs somewhere to bill"
    Cloud spend is tracked and budgeted per client — the ledger, the budget window,
    the threshold ladder and the spend report are all keyed on one — so provisioning
    needs a client to charge. A hashlist created without one is skipped, and on the
    autoscaler path it used to be skipped with no message at all.

    You have two ways to resolve it, depending on whether your deployment models
    clients:

    - **You do use clients:** set `require_client_for_hashlist` to `true` so a
      hashlist cannot be uploaded without one. Note this gates *new uploads only* —
      it does not repair hashlists that already exist without a client.
    - **You don't use clients:** create one (call it something like
      `Unassigned Work`), give it a budget, and nominate it as
      **`cloud_default_client_id`**. All work with no client of its own bills there.
      Budgets, instance caps and the threshold ladder apply to it exactly as they
      would to any other client, so the spend is bounded and shows up in the report
      as an ordinary row.

    A hashlist's own client always wins — the fallback never redirects spend away
    from an engagement that has one. Leave `cloud_default_client_id` unset and
    behaviour is unchanged: unassigned work simply cannot rent capacity.

!!! tip "Turn on cloud burst once, not per job"
    `cloud_burst_enabled` is per job and defaults to off, which is right when cloud
    burst extends a fleet you already own and backwards when renting is the only way
    work ever runs. Set **`cloud_default_burst_enabled`** (Cloud Provisioning →
    System → Cloud-only deployments) and every job is treated as opted in, including
    jobs already sitting in the queue. Client budgets and both instance caps still
    apply on top of it — this decides which jobs are *considered* for renting, not
    how much may be spent.

    Leave it off in a hybrid deployment and tick the box per preset instead: jobs
    inherit the flag from the preset they are created from, so it is one tick per
    preset rather than one per job.

### The safety rails are different without on-prem agents

Two of the three brakes on runaway renting are computed from on-prem agents and are
therefore inert here:

- the "an on-prem agent is idle, don't rent" check, and
- `skip_if_finishing_within_seconds`, which needs on-prem throughput to project from.

What still applies is the client budget, the deployment-wide cap, and the per-job instance
cap. **Set `cloud_default_max_instances_per_job`** (shipped default `2`) and
`cloud_global_concurrent_instance_cap` accordingly — a blank per-job cap inherits the
default rather than meaning unlimited, but the deployment cap ships at `0`, which *is*
unlimited.

Also raise `cloud_commissioning_grace_minutes` (default 30) rather than lowering it. A
rented instance must download its files and run a benchmark before it can be given work,
and destroying it mid-startup means paying for the launch and then paying again for its
replacement.

### How short a rental is worth making

| Setting | Default | Effect |
|---|---|---|
| `cloud_max_commissioning_pct` | `33` | The largest share of a rental's bill that may go on commissioning before the rental is refused as not worth making. |

A rented GPU spends roughly 20 minutes booting, joining the VPN, registering, downloading
wordlists and benchmarking before it can consume a single candidate. That cost is paid
again on every rental, so short rentals are mostly setup: at a 30-minute lifetime, two
thirds of the bill buys nothing.

This setting turns that into a rule. At the default `33`, commissioning may be at most a
third of the bill, which puts the **minimum useful rental at about an hour**. Anything
shorter is refused before the instance is created, with a message naming both the length on
offer and the minimum, and a `cloud_rental_too_short` diagnostic on the job.

- **Raise it** to accept shorter, less efficient rentals — useful if you want small,
  frequent bites and do not mind the overhead.
- **Lower it** to demand longer, more efficient ones.
- **Set it to `0`** to remove the efficiency rule entirely. This does *not* remove the
  floor: a rental still has to be long enough to be given at least one chunk of work, or
  the instance would bill without ever being handed anything.

If provisioning starts refusing after an upgrade, this is the likely cause, and the fix is
usually to raise `max_instance_ttl_minutes` on the client rather than to change this.

### When nothing happens

Open the job. Provisioning refusals are shown on the job detail page — budget reached,
instance cap reached, waiting for an instance to start up, no capacity at the provider,
or instances that could not connect back. That panel is the first place to look; the
server log is no longer the only record.

If the job shows nothing at all, it was never considered cloud-eligible. Click
**Provision now** on the job, which names the specific precondition that failed —
including the two that are otherwise invisible: no client to bill, and cloud burst
never enabled.

## What cloud provisioning leaves behind, and what cleans it up

Renting a GPU creates two rows that outlive the rental, and both used to accumulate
forever. What happens to them now:

### Claim codes

Each launch **attempt** mints a single-use claim voucher before the provider is called —
one per candidate offer, so a provision that walks a run of capacity refusals mints one for
each. Two things now clear them up:

| When | What happens |
|---|---|
| The attempt definitively fails — no capacity, budget refused | The code is **deactivated immediately**. It can never be redeemed. |
| The instance is torn down — job finished, idle drain, ready deadline | Same, at teardown. |
| The launch outcome is **ambiguous** (a timeout) | The code is deliberately **left live**. The instance may be running and its agent still has to register with it; the reaper kills the code once it knows the outcome. |
| A code has been expired and unredeemed for `voucher_retention_days` | Deleted by a daily sweep. |

Expired codes no longer appear in the voucher list at all — previously the list filtered
only on `is_active`, which does not imply usable, so every dead code from every failed
launch was still displayed.

`voucher_retention_days` defaults to **30**; `0` keeps them forever. **Redeemed vouchers are
never swept at any setting** — they record which agent joined with which credential.

### Agent records

A rented GPU registers as an agent. On teardown that row is **retired**, not deleted:
deleting it would sever the rental's cost attribution and take the agent's benchmark
history with it, and those benchmarks are what make cost-per-work ranking accurate.

Retired agents are **hidden from the agent list by default**. They are still there, and
still reachable, so historical job views continue to show which agent ran which task.

## Verifying before you spend

Run the pre-flight from the provider page. What it can check differs by provider:

| Provider | Pre-flight reports |
|---|---|
| **AWS** | Whether credentials resolve (`sts:GetCallerIdentity`), the applied or default GPU quota and current usage, and exactly which IAM permissions are missing — via a real `DryRun` |
| **RunPod** | That the key is accepted, read scope on pods, **write** scope (probed with a deliberately schema-invalid create that cannot succeed), and the account credit balance |
| **Vast.ai** | That the key is accepted, and the prepaid balance |

Anything inconclusive is treated as **failure**, not success: "unknown" is not "allowed". On
AWS that makes pre-flight stricter than launching — see
[the caveat](cloud-aws.md#what-is-required-and-what-merely-degrades).

Then rent one cheap instance with a short TTL and confirm teardown by killing the backend
immediately after launch — the in-guest watchdog should still destroy it.

**Before spending anything at all, run the $0 rehearsal.** `scripts/cloud-e2e-test.sh`
drives a complete rental against the built-in mock provider, which launches a local
test-mode agent instead of renting hardware. It creates its own client, hashlist and job
through the REST API and asserts each step of the lifecycle — TTL sizing, the chunk clamp
against the teardown slack, the crack handshake, the teardown reason, and that the unused
reservation is refunded exactly. It also injects a deliberate crack-processing fault to
confirm a handshake that can never be satisfied is given up on immediately rather than
holding the instance until the stale-processing timeout.

One limit worth knowing: the mock agent never runs hashcat, so it emits synthetic hash
values that match nothing and a hashlist will read **0 cracked** however well the run goes.
The rehearsal proves provisioning, sizing, the handshake and teardown. It cannot prove that
cracked passwords are *recorded* — only a real agent can do that.

There is a second, manual script alongside it. `scripts/cloud-only-rehearsal.sh` configures a
$0 mock-provider deployment with no on-prem GPUs, following the
[cloud-only checklist](#cloud-only-deployments-no-on-prem-gpus) above, and then hands you a
checklist to work through by hand. Use `cloud-e2e-test.sh` for regression checking and this
one to rehearse the experience an operator will actually have.

## Related

- [Cloud Agent VPN](cloud-vpn.md) — required before any provider can be enabled
- [AWS EC2](cloud-aws.md) · [RunPod](cloud-runpod.md) · [Vast.ai](cloud-vastai.md) — per-provider setup
- [Cloud Agent Deployment](../../agent-guide/cloud-deployment.md) — what runs inside a rented instance
- [Cloud GPU Provisioning](../../reference/architecture/cloud-provisioning.md) — design rationale
- [Job Priority](../advanced/job-priority.md) — the scale the minimum-priority rule is absolute against
