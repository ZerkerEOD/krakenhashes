# RunPod

!!! warning "Beta — implemented but never paid for"
    Both RunPod kinds are written against the documented API with no account to verify them
    on. The rental lifecycle is unproven against the live service. Start with a small budget
    and a one-instance cap, and watch instances reach `terminated` rather than assuming they
    will. See
    [Maturity](cloud-providers.md#maturity-which-providers-have-actually-been-paid-for).

RunPod is configured as **two separate provider kinds**, not one with a tier setting:

- **`runpod` — Secure Cloud.** RunPod's own datacenters, single-tenant per host, covered by
  their SOC 2 Type II, ISO 27001 and PCI DSS attestations. Treat it like AWS.
- **`runpod_community` — Community Cloud.** Peer-operated machines. See the
  [trust-tier table](cloud-providers.md#trust-tiers-which-providers-carry-a-warning-and-why);
  none of those attestations extend to this tier.

Splitting them is what lets a client allowlist Secure without ever being exposed to Community,
and what keeps the consent chain attached to the tier that needs it.

## Teardown differs sharply between the two tiers

This is the difference that costs money, so it comes first.

| | `runpod` (Secure) | `runpod_community` |
|---|---|---|
| In-guest deadline kills hashcat | Yes | Yes |
| Pod can delete itself | Optional (see below) | **Never** |
| Backend reaper | Yes | Yes — **and it is the only thing that stops billing** |

!!! danger "Community teardown is reaper-only"
    RunPod issues **no per-pod scoped credential** — there is no equivalent of Vast.ai's
    `CONTAINER_API_KEY`, which can delete only its own instance. The only key that can delete
    a pod is **account-scoped**, and on Community the host operator has root over the
    container and would read it out of the environment. That key can create and delete every
    other pod on your account, so KrakenHashes never places one in a Community pod.

    The consequence: the in-guest deadline still kills hashcat and ends the data exposure, but
    **it cannot stop the pod billing**. If the backend is down or wedged, a Community pod bills
    until you destroy it by hand from the Cloud Fleet page.

    Give any client allowed on this tier a **low `max_instance_ttl_minutes`** and a small
    budget cap.

Setting `allow_in_guest_self_destruct` on a Community config does nothing: the refusal is
unconditional and is enforced where the key is *held*, not where it is injected, so no future
change to the injection path can reintroduce it. A warning is logged if you set it anyway.

On **Secure**, `allow_in_guest_self_destruct` adds the third teardown rail so a pod can
`DELETE` itself when it loses contact, matching Vast.ai.

!!! warning "Supply a narrower key for the guest"
    The key placed inside the pod can create and delete **every pod on your account**. Supply
    a separate one scoped to pod read/write as `self_destruct_api_key`:

    ```json
    { "api_key": "<provisioning key>", "self_destruct_api_key": "<pod read/write only>" }
    ```

    If you enable in-guest self-destruct without supplying one, the **provisioning key** is
    used instead and a warning is logged. A bare string in the credentials field is also
    accepted and read as the provisioning key alone.

---

## Account setup

1. Use a **dedicated RunPod account. This is a requirement, not advice.** RunPod has no way to
   tag a pod as ours, so ownership is decided client-side by matching pod **names** against the
   anchored `kh-xxxxxxxx-xxxx-xxxx` label shape. A name is guessable where a tag is not, and
   orphan reconciliation destroys unrecognised pods matching that shape. AWS gets a
   provider-enforced tag filter; RunPod has nothing equivalent.
2. Fund it with a **fixed prepaid balance and no auto-refill**. Pre-flight tries the GraphQL
   `myself` query for a balance; if your account does not expose one, the first sign of an
   empty account is a `402` at pod-create time.
3. Create an API key with **pod read/write**. Pre-flight probes for write scope with a
   deliberately schema-invalid create, so a read-only key is caught at configuration time
   rather than at the first launch.

RunPod has no IAM equivalent — read versus write on pods is the only scope distinction that
exists.

## Settings

```json
{
  "data_center_ids": ["EU-RO-1", "US-KS-2"],
  "gpu_type_ids": ["NVIDIA GeForce RTX 4090"],
  "container_disk_gb": 60,
  "container_disk_cents_per_gb_month": 10,
  "interruptible": false,
  "allow_in_guest_self_destruct": false
}
```

| Field | Default | Notes |
|---|---|---|
| `data_center_ids` | `[]` | **Empty is the widest setting** — see below |
| `gpu_type_ids` | `[]` | Empty means every type the tier carries |
| `container_disk_gb` | `0` | A floor; the job's fileset may raise it |
| `container_disk_cents_per_gb_month` | `10.0` | Storage term of the reservation |
| `interruptible` | `false` | Leave it false — see below |
| `allow_in_guest_self_destruct` | `false` | Secure only; ignored on Community |

There is **no rate table**, unlike AWS: RunPod returns live prices, so declaring your own
would be a second source of truth that can only ever be more wrong. There is also no volume
setting — network volumes **outlive the pod** and would retain cracked plaintexts after
termination, so none is ever attached.

!!! warning "`data_center_ids` works the opposite way round from AWS zones"
    **Empty is the WIDEST setting here, not the narrowest.** With no data centres pinned,
    RunPod's own scheduler may place a pod anywhere. On AWS, omitting the subnet still lands
    you in exactly one availability zone, so pinning *widens* the pool; on RunPod pinning
    *narrows* it.

    Set it for data residency, or when you would rather the launch retry loop walk genuinely
    independent pools than re-hit one exhausted global placement.

!!! danger "Leave `interruptible` off"
    It requests spot pods, but the offer contract cannot yet express preemption honestly: a
    pod with no maximum duration is read as **unbounded**, which is the exact opposite of the
    truth for a spot pod. An interruptible pod currently passes the "must survive twice the
    TTL" filter it should fail.

## Picking data centres from the UI

**Admin → Settings → Cloud Provisioning → Providers → (edit a RunPod provider) → Data centres
and GPU types.**

"Check availability" renders a **data centre × GPU type** grid from live data. Unlike AWS, the
two axes are chosen **independently** — tick the data centres you accept and the GPU types you
accept, and every combination is allowed. AWS instead lets you pick types per zone.

| Signal | How much to trust it |
|---|---|
| **Offered** | Measured. RunPod reports it per type per tier |
| **Available count** | Measured, and live |
| **Live price** | Measured. There is no configured rate to compare against |

A synthetic **"Anywhere (RunPod chooses)"** row appears when nothing is pinned. Selecting it
is the widest setting; it is not a real data centre id and is never sent as one.

The screen reads the **saved** provider config, so save before checking.

## Operational caveats

- **No provider-enforced TTL.** RunPod has no `autoTerminate` or `expiresAt` field, so teardown
  rests on the in-guest deadline and the backend reaper. Prefer shorter TTLs here than you
  would on AWS.
- **A stopped pod still bills**, with volume disk charged at roughly double the running rate.
  KrakenHashes always terminates and never stops.
- **Billing granularity is one hour**, so cost-so-far reads as unknown for any pod that lived
  less than that. Accrual stays wall-clock based.
- **No idempotency key on pod create.** A lost response can leave a pod billing whose id was
  never seen. The adapter adopts by label before creating and reconciles by name on an
  ambiguous failure, and a double launch shows up in the fleet inventory as a duplicate that
  gets reaped as an orphan — but the window is real, and it is bounded by one reaper sweep in
  the good case.
- **Offers are single-GPU** in this version. It is not yet confirmed whether RunPod quotes
  `lowestPrice` per GPU or per pod, and being wrong on a 4-GPU pod would under-reserve a budget
  cap fourfold. Every launch compares the pod's actual `costPerHr` against the reserved rate
  and logs any drift over 10%.
- **Pod list pagination is undocumented.** The adapter warns loudly if a page comes back at a
  suspiciously round count, because a silently truncated list would make live pods look like
  orphans — or hide them.

## Related

- [Cloud GPU Providers](cloud-providers.md) — budgets, provisioning rules, trust tiers
- [Cloud Agent VPN](cloud-vpn.md) — the VPN credential every provider needs
- [Cloud GPU Provisioning](../../reference/architecture/cloud-provisioning.md) — adapter design notes
