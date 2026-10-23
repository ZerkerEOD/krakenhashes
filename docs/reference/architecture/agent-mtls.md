# Agent mTLS Authentication

KrakenHashes agents can authenticate to the backend with **mutual TLS (mTLS)** using a per-agent
client certificate, in addition to the original API-key method. This hardens agent authentication
and gives clean per-agent revocation. The web UI continues to use JWT.

It runs on the **existing HTTPS port** — there is no new port to open — and agents **migrate to it
automatically**. For most operators there is nothing to configure.

## How it works, on one port

The backend's HTTPS listener (the agent port, default `31337`) is set to **verify a client
certificate only if the peer presents one**:

- The web UI reaches the backend through nginx, which presents no client certificate, so it passes
  through untouched and keeps using JWT.
- A migrated agent presents its per-agent certificate; the backend verifies it against the internal
  CA and treats the connection as mTLS.
- A not-yet-migrated agent authenticates with its API key, exactly as before. This covers both a
  3.x agent that has not renewed yet and a **2.x agent**: 2.x agents present the shared bootstrap
  certificate (CommonName `KrakenHashes Client`) on every connection. The backend recognises that
  certificate as "no per-agent identity" and falls back to the API key rather than rejecting it,
  so a 2.x agent keeps working against a 3.x backend until you update it. This is a transitional
  bridge, not a permanent API-key tier: the agent shows as "Update available", and the recommended
  fix is to move it onto the launcher, which swaps the binary and keeps it current. On its next
  connection as a 3.x agent it migrates to per-agent mTLS.

Because the certificate is verified only when present, API-key agents and mTLS agents coexist on
the same port with no cutover and no firewall change. A verified certificate whose CommonName is
anything other than an agent ID or the shared bootstrap name is rejected.

## Automatic migration

The backend advertises, on `/api/agent/config`, whether it verifies per-agent certificates. When an
agent sees that and finds that its current certificate is not its own per-agent certificate (its
CommonName is not the agent's ID), it renews once over the plain-HTTP bootstrap port to obtain a
per-agent certificate, reloads it, and reconnects presenting it. From then on it is an mTLS agent.

- **New agents** receive a per-agent certificate at registration and use mTLS from their first
  connection.
- **Existing agents** migrate on their next connection after the server is upgraded. No admin
  action, no new port, no reconfiguration.

## Requires the internal self-signed CA

Per-agent certificates are issued and verified by the backend's **internal self-signed CA**, so
mTLS is active only when `KH_TLS_MODE=self-signed` (the default). In `provided` or `certbot` modes
the public certificate chain is not a CA the backend can issue agent certificates with, and it must
never be used to verify client certificates, so the backend reports mTLS as off, agents stay on
API-key authentication, and no migration is attempted.

## Revocation and the revoke action

Revocation is one mechanism for both the certificate and the operator-facing **revoke** action:

- Revoking an agent invalidates its API key (via an `is_revoked` gate on the key lookup) **and**
  adds its certificate serial to a revocation list the TLS handshake consults.
- A revoked agent's live WebSocket is severed immediately, and the agent row is retained for
  history (it moves to the Revoked view in agent management).
- Bringing a revoked agent back requires a new claim code and fresh credentials.

Revoke is distinct from **disable**. Disable only pauses scheduling; the agent's credentials stay
valid (useful for update windows). Revoke is the hard kill.

## What mTLS does and does not change for abuse resistance

The project's anti-abuse guarantee does **not** come from mTLS. It comes from two things that are
unchanged here:

- the agent **pins the server's internal CA**, so the backend cannot be impersonated or hidden
  behind a TLS-terminating proxy, a CDN, or a DNS swap; and
- the agent reaches the backend **only over the mandatory VPN**.

Together those force agents to make an identifiable connection to the real server, which is what
lets a misused deployment be traced back to whoever runs it. A per-agent credential identifies the
agent host, which in a botnet is usually an unwitting victim rather than the operator, so it is not
the attribution anchor.

mTLS adds, on top of that guarantee, the backend cryptographically verifying each agent's identity
and clean per-agent revocation. It is defense in depth and operational hygiene, not the thing that
stops abuse. Two invariants preserve the guarantee: the agent must keep pinning the server CA, and
its certificate must reach the backend directly (agents connect straight to the agent port over the
VPN, never through the nginx reverse proxy, which would terminate the certificate).
