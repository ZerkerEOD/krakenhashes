# Vast.ai

!!! warning "Experimental — implemented but never paid for"
    Fully implemented; never once driven end to end with a funded account. The rental
    lifecycle is unproven against the live marketplace. Start with a small budget and a
    one-instance cap, and watch instances reach `terminated` rather than assuming they will.
    See [Maturity](cloud-providers.md#maturity-which-providers-have-actually-been-paid-for).

    **If you run this, please say what happened** — that is the only thing that moves Vast.ai
    off this list. File a bug report, and send diagnostics by Discord DM rather than
    attaching them:
    [Reporting a problem](cloud-providers.md#reporting-a-problem-with-an-experimental-provider).

!!! danger "Every Vast.ai host has root over your container"
    Vast.ai is a marketplace of **individually-owned consumer machines**. The owner of the
    machine your job lands on can read the hashes, wordlists, potfiles and cracked plaintexts
    on it. The protection is a terms-of-service clause, not an isolation boundary.

    This tier requires three separate opt-ins — provider, per client, and per job — each
    attributed in the audit log. Do not use it for production or client engagement data.

## Account setup

1. Use a **dedicated Vast.ai account**. Orphan reconciliation destroys anything on the account
   that KrakenHashes does not recognise.
2. Fund it with a **fixed prepaid balance and no auto-billing card**. Vast.ai offers no
   spending cap; a prepaid balance is the only hard ceiling available. At zero balance Vast
   stops instances automatically.
3. Create a scoped API key with `misc`, `user_read`, `instance_read`, `instance_write`.
   **Offer search lives under `misc`, not `instance_read`** — this is easy to get wrong.

## Credentials

Unlike AWS and RunPod, Vast.ai takes a **bare API key string** — not a JSON document.

```
abcdef0123456789abcdef0123456789abcdef01
```

It is AES-256-GCM encrypted at rest and never returned by the API.

!!! info "Vast.ai rate-limits without telling you when to retry"
    Vast enforces a minimum interval between requests per endpoint and returns `429` with **no
    `Retry-After` header**. KrakenHashes paces itself at one request every three seconds to
    stay under it, so offer searches are not instant.

## Settings

```json
{
  "countries": ["US", "DE"],
  "gpu_models": ["rtx_4090", "rtx_5090"],
  "denied_gpu_models": [],
  "min_reliability": 0.9,
  "allow_unverified": false,
  "allow_residential": false
}
```

**Every list empty is the default and means no restriction** — the widest pool. An empty
`gpu_models` keeps working as you add cards to your fleet; an explicit list freezes the
selection.

The zero value is exactly today's behaviour: **verified datacenter hosts only, every country,
every card, no reliability floor.**

| Setting | What it does |
|---|---|
| `countries` | Vast.ai geolocations, matched case-insensitively as a substring, so `US` matches `US, Texas`. Doubles as your data-residency control |
| `gpu_models` / `denied_gpu_models` | Matched on a normalised key, so `rtx_4090` matches both `NVIDIA GeForce RTX 4090` and `RTX 4090`. **Deny always wins** — it is the emergency lever for "that card keeps failing" |
| `min_reliability` | Vast's own host score, 0–1. Hosts that report **no** score are kept, not dropped. Trades a little availability for fewer dead rentals: a host that drops the instance mid-chunk has still been paid for its commissioning |
| `allow_unverified` | Opens the unverified tier. See below |
| `allow_residential` | Opens non-datacenter hosts. See below |

!!! danger "What opening the tiers actually costs"
    This is the single largest availability increase available on Vast.ai, and it places client
    hashes, wordlists, potfiles and cracked plaintexts on machines Vast.ai has **not verified**.
    Every Vast host already has root over the container — these toggles remove the one filter
    that keeps it to hosts Vast has checked.

    Some of the extra availability is also illusory: the unverified tier is where "stuck
    connecting" and "bad driver" reports concentrate, and a rental that never reaches useful
    work still costs you its commissioning.

## Picking countries and cards from the UI

**Admin → Settings → Cloud Provisioning → Providers → (edit a Vast.ai provider) → Countries
and GPU models.**

"Check availability" renders a **country × GPU model** grid with live rentable counts. The two
axes are chosen **independently** — tick the countries you accept and the cards you accept, and
every combination is allowed. AWS instead lets you pick types per zone.

| Signal | How much to trust it |
|---|---|
| **Offered** | Measured, from the live marketplace |
| **Available count** | Measured, and live. The most useful column here |
| **Live price** | Measured. There is no configured rate to compare against |

The probe deliberately relaxes your own country and model filters so you can see what you are
excluding — but it **does not relax the trust tier**. If `allow_unverified` is off, the grid
shows verified hosts only, because widening the grid past the boundary you set would advertise
capacity you have decided not to use.

The grid caps at 12 GPU-model columns and warns when models are hidden.

The screen reads the **saved** provider config, so save before checking.

## Operational caveats

- **No provider-enforced TTL.** Teardown rests on the in-guest deadline and the backend reaper.
- **Storage bills from contract creation and keeps billing while the instance is stopped**, so
  KrakenHashes always **deletes** and never stops.
- **No idempotency token on create**, and the id you get back is `new_contract`, not `id`.
- **`exited`, `unknown` and `offline` are terminal.** An instance in one of those states never
  recovers, so it is torn down rather than waited on.
- **Unprivileged containers** — no `/dev/net/tun`, no `NET_ADMIN`, and the create API silently
  discards `--cap-add`/`--device`. This is why the VPN runs in userspace mode and why
  [OpenVPN cannot be supported](cloud-vpn.md#supported-vpns).
- **Disk is sized once and cannot grow** after creation.

## Related

- [Cloud GPU Providers](cloud-providers.md) — budgets, provisioning rules, trust tiers
- [Cloud Agent VPN](cloud-vpn.md) — the VPN credential every provider needs
- [Cloud GPU Provisioning](../../reference/architecture/cloud-provisioning.md) — adapter design notes
