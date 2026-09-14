# Cloud Agent VPN

Rented GPU agents reach the KrakenHashes server over **your existing VPN**. This page covers
which VPNs are supported, how to obtain a credential for each, and why OpenVPN is not on the
list.

Part of [Cloud GPU Providers](cloud-providers.md). For what the rented container does with
the credential once it has it, see [Cloud Agent Deployment](../../agent-guide/cloud-deployment.md).

## A VPN is mandatory, and there is no bypass

KrakenHashes does not build a VPN and does not offer a "direct connection" mode. The server
is meant to be unreachable from the internet, and a rented GPU is third-party hardware —
keeping it on your overlay is what stops a machine someone else operates from talking to a
public endpoint.

This is enforced at **two independent points, both fail-closed**:

| Gate | When | Message |
|---|---|---|
| Enabling a provider | Before the config can be saved as enabled | *"a VPN provider is required: the backend is not internet-exposed, so an instance that cannot join your VPN would bill until its watchdog fired"* |
| Minting the credential | At provision time, before any provider API call | *"no VPN provider configured; refusing to launch an instance that cannot reach the backend"* |

The second gate runs **before** the offer is accepted and before any money is committed, so
a missing credential costs nothing. The mock provider is **not** an exemption: it has a
credentials exemption (it authenticates to nothing) but the VPN check applies to it too.

!!! note "Why fail closed rather than launch and retry"
    An instance that cannot join the VPN can never reach the backend. It would boot, fail to
    register, and bill until its watchdog fired — strictly worse than not launching at all.

A **disabled** provider config may be saved without VPN fields. The validation runs only on
the transition to enabled.

## Supported VPNs

| Provider | Per-instance credential | Auto-deregistration |
|---|---|---|
| **Tailscale** (recommended) | OAuth client → one-off ephemeral tagged key per instance | 30–60 min, or instant on logout |
| **NetBird** | PAT → one-off ephemeral setup key per instance | ~10 min |
| **WireGuard** | none — static operator config | none, manual |

All three run in **userspace mode** inside the rented container, exposing a local SOCKS5
proxy the agent dials through. This is not a preference: Vast.ai and RunPod containers are
unprivileged, with no `/dev/net/tun` and no `NET_ADMIN`.

Prefer the OAuth/PAT path. A reusable Tailscale auth key is capped at **90 days**, so it is
a scheduled outage; KrakenHashes tracks its expiry, warns as it approaches, and **refuses to
provision once it lapses** rather than launching an instance that can never connect.

!!! danger "OpenVPN is not supported"
    Vast.ai runs unprivileged containers with no `/dev/net/tun` and no `NET_ADMIN`, and its
    API silently discards `--cap-add`/`--device`. OpenVPN cannot work there, and the only
    "userspace" path for it is unmaintained out-of-tree patches to a security-critical
    binary.

    The same applies to RunPod, whose pods are unprivileged containers of the same shape.
    `wireguard-go` and `boringtun` are excluded for the related reason that they still create
    a TUN device; `wireproxy` is used instead.

    This was a founding constraint of the feature rather than something removed later. If you
    need a fourth VPN profile, the requirement it has to meet is a **userspace network stack
    exposing a SOCKS5 listener**.

## Credential kinds

The `vpn_credential_kind` field decides whether KrakenHashes can mint a short-lived
credential per instance or must reuse one you supplied.

| Kind | Blast radius if a rented host reads it | Deregistration |
|---|---|---|
| `oauth` | Minimal — the key is already consumed, tag-scoped by ACL, and evaporates minutes after the instance dies | Automatic |
| `pat` | As above | Automatic |
| `reusable_key` | **Every instance shares one key.** Larger, and it expires | Automatic per node, key persists |
| `static_config` | Largest — a long-lived private key, no per-instance scoping | **None, manual** |

Which kinds are legal for which provider:

| VPN provider | Allowed kinds |
|---|---|
| `tailscale` | `oauth`, `reusable_key` |
| `netbird` | `pat`, `reusable_key` |
| `wireguard` | `static_config` |

!!! warning "That pairing is enforced by the UI only"
    The backend does not reject a mismatched combination such as `wireguard` + `oauth` — it
    would pass the credential through verbatim and the instance would fail to connect. If you
    configure providers through the API rather than the admin screen, honour the table
    yourself.

---

## Tailscale (recommended)

### OAuth client — the preferred path

1. In the Tailscale admin console, go to **Settings → OAuth clients** and create a client.
2. Grant it the **`auth_keys`** scope. Nothing else is needed.
3. **Select the tag** the client may issue keys for, for example `tag:kraken-worker`. An
   OAuth client with no tag selected can authenticate but cannot mint a usable key.
4. Define that tag in your tailnet ACL policy and give it only the access a cracking agent
   needs — reaching the backend's port, and nothing else.
5. Paste the credential into KrakenHashes as **`client_id:client_secret`**, joined by a
   single colon.

```
<client-id>:<client-secret>
```

Both halves come from the OAuth client you just created — Tailscale shows the secret once,
at creation.

!!! warning "The colon format is validated, the rest is not"
    The credential is split on `:` and rejected with *"tailscale OAuth credential must be
    'client_id:client_secret'"* if that does not yield exactly two parts. A wrong scope or an
    unselected tag is **not** caught at save time — it surfaces as a failed mint at
    provisioning.

6. Set **VPN tag or group** to the same tag. This is required for Tailscale OAuth and
   enabling the provider is refused without it: *"Tailscale OAuth requires a tag (e.g.
   tag:kraken-worker); OAuth-minted keys are always tagged"*.

Each launch then mints one key with `reusable:false`, `ephemeral:true`, `preauthorized:true`
and your tag, expiring in **15 minutes or less** — long enough to boot and register, and
worthless afterwards.

### Self-hosted control plane

Set **`tailscale_login_server`** in the provider settings to point at a Headscale or
self-hosted coordination server. Leave it blank for Tailscale's SaaS.

### Reusable key (fallback)

Create a **pre-authorized, tagged** auth key in the admin console and supply it as
`reusable_key`. Also set **VPN credential expires at** to the key's real expiry in RFC3339
form, because this is the one kind whose expiry KrakenHashes enforces — see
[Expiry](#expiry-is-tracked-for-one-kind-only).

---

## NetBird

1. Create a **personal access token** or a service-user token in the NetBird dashboard.
2. Supply it as kind `pat`.
3. Set **`netbird_management_url`** if you self-host. Defaults to `https://api.netbird.io`.

Each launch mints a setup key with `type: "one-off"`, `usage_limit: 1` and `ephemeral: true`.

!!! info "What actually bounds a NetBird key"
    NetBird's `expires_in` has a documented **minimum of 86400 seconds (1 day)**, so a
    15-minute key is not possible. The real bound is `usage_limit: 1` plus `ephemeral: true`,
    which removes the peer roughly 10 minutes after it goes offline.

!!! warning "NetBird ignores the tag on the reusable-key path"
    With a `reusable_key`, the in-guest startup passes only `--setup-key` and
    `--management-url` and never reads the tag field, so **VPN tag or group** does nothing.
    It is honoured on the `pat` path.

!!! info "NetBird has no DNS in netstack mode"
    NetBird's userspace mode provides no DNS. Pin the backend's **overlay IP** as the cloud
    host and make sure the server certificate has a matching IP SAN.

---

## WireGuard

Supply the **full peer configuration** as kind `static_config`. It is written verbatim into
the container at `/etc/wireproxy/wireproxy.conf` with a `[Socks5]` stanza appended, and
`chmod 600`.

```ini
[Interface]
PrivateKey = <base64 private key>
Address = 10.0.0.8/32
DNS = 10.0.0.1

[Peer]
PublicKey = <server public key>
Endpoint = vpn.example.com:51820
AllowedIPs = 10.0.0.0/24
PersistentKeepalive = 25
```

!!! danger "WireGuard is the least-safe option, deliberately offered anyway"
    There is **no per-instance credential and no automatic deregistration**. Every rented
    instance receives the same long-lived private key, and the host operator on a
    peer-operated tier has root over the container and can read it. Removing a peer is a
    manual step you have to remember after every rental.

    Use it when you already run WireGuard and cannot add another overlay. Prefer Tailscale or
    NetBird for anything touching client data.

The config is **never parsed by the backend** — a malformed file is not detected until the
container fails to bring the tunnel up and self-destructs on *"VPN unavailable"*.

---

## Expiry is tracked for one kind only

**VPN credential expires at** (RFC3339) drives a countdown in the admin UI and a hard refusal
once it passes:

> the reusable `<provider>` key expired at `<time>`; rotate it before provisioning

!!! warning "It is enforced for `reusable_key` and nothing else"
    An expiry set on a `pat` or `static_config` credential is stored and displayed but never
    acted on. The UI only offers the field for `reusable_key` for that reason; setting it
    through the API on another kind gives you a countdown that means nothing.

Supplying a new credential resets the expiry. Leaving the secret field blank on an edit keeps
both the stored secret and its expiry.

## The backend's address on your VPN

Cloud agents connect to the backend at its **VPN** address, and the agent sets no explicit
`ServerName`, so SNI is whatever host it dials. That address must be in the certificate's
subject alternative names.

Add it in **Admin → Settings → Server Certificate** and click **Apply & Reissue** — for
example the Tailscale name `kraken.tailnet-xxxx.ts.net` and its CGNAT address
`100.113.129.115`. CGNAT addresses (`100.64.0.0/10`) are permitted out of the box, since that
is what Tailscale uses and what NetBird's default account network is drawn from.

!!! warning "Self-hosted NetBird may sit outside CGNAT"
    A self-hosted NetBird can be configured with any network range, and it is easy to pick one
    that is not private. `100.133.64.0/19` looks like CGNAT because it starts with `100.`, but
    CGNAT stops at `100.127.255.255`. KrakenHashes refuses addresses above that, with no
    override.

    Check with `netbird status` (the `NetBird IP` line) or `ip -o -4 addr show wt0`. If your
    range is outside `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` or `100.64.0.0/10`,
    change the VPN's network range. Renumbering re-allocates every peer, so update the backend
    VPN host and the certificate SAN list afterwards.

The reissue is immediate and non-disruptive: it uses the existing certificate authority, so
enrolled agents are unaffected and the backend needs no restart. You do **not** need to set
this before first boot, and you must not delete the certs directory to change it.

!!! note "Enabling a provider only checks that a VPN host is configured"
    KrakenHashes requires the backend VPN host to be non-empty before a provider can be
    enabled, but it does not verify that the address appears in the certificate. Check it
    yourself after adding the provider:

    ```bash
    openssl s_client -connect <vpn-address>:31337 </dev/null 2>/dev/null \
      | openssl x509 -noout -text | grep -A1 "Subject Alternative Name"
    ```

    If a rented instance cannot connect, its reported address also appears under
    **Discovered addresses** on the Server Certificate page.

---

## When agents launch but never register

!!! warning "Keep the NetBird client reasonably current"
    A Dockerised backend behind NetBird works because NetBird marks traffic in `prerouting`,
    *before* Docker's published-port DNAT rewrites the destination. That mark is what lets the
    packet through the forward path after the rewrite. Older clients did not do this, and on
    those the agent sees `operation timed out` — a **drop**, not a refusal — while the peer,
    the handshake and the ACL all look healthy.

    Check the client version on the backend host before anything else:

    ```bash
    netbird version
    ```

    The NetBird maintainers describe this marking as applying to **peer ACLs** (destination is
    the peer itself) and not to route ACLs. Reaching the backend at its own overlay address is
    the peer-ACL case, so it is covered.

!!! tip "Testing from the Docker host proves nothing"
    Locally-originated traffic never traverses the forwarding path, so
    `curl https://<overlay-ip>:31337/` succeeds on a host where every remote peer is being
    dropped. Always test from a **different** peer:

    ```bash
    curl -m 10 http://<backend-overlay-ip>:1337/ca.crt
    ```

    If that times out while the backend answers on the host itself, capture on both interfaces
    to see where the packet dies — and check that nothing has left a stray interface in the
    container's network namespace (`docker exec <app> ip route`). A second route for the
    overlay range inside the container will silently blackhole every reply.

A container that cannot bring its tunnel up **self-destructs** rather than sitting there
billing, so a broken VPN shows up as instances that terminate within a few minutes with
reason *"VPN unavailable"* — not as a growing bill.

## Known limitation: keys are not actively revoked

KrakenHashes records a reference to each minted credential on the instance row, but **nothing
reads it back to revoke the key**. De-registration relies entirely on the ephemeral flags:
Tailscale removes the node 30–60 minutes after it goes offline, NetBird roughly 10 minutes.

In practice the minted keys are single-use and already consumed by the time an instance is
running, so there is nothing useful left to revoke. It matters for `reusable_key` and
`static_config`, where the credential outlives the rental by design — which is the reason to
prefer the OAuth and PAT paths.

## Related

- [Cloud GPU Providers](cloud-providers.md) — budgets, trust tiers, provisioning rules
- [Cloud Agent Deployment](../../agent-guide/cloud-deployment.md) — what the container does with the credential
- [SSL/TLS Setup](ssl-tls.md) — certificate SANs and reissue
- [Security Guide](../security.md) — the address ranges accepted in a certificate
