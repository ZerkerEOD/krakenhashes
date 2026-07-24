# Changelog

All notable changes to KrakenHashes, newest release first. This file tracks tagged
releases; the complete per-tag history is available on the [GitHub Releases page](https://github.com/ZerkerEOD/krakenhashes/releases).
Patch releases are rolled into their minor version's section.

## 2.1.2 - 2026-06-10

- Filtered wordlists now regenerate automatically when their source wordlist changes, appending only the new entries incrementally instead of rebuilding from scratch ([#40]).

## 2.1.1 - 2026-06-05

- Fixed single-agent job dispatch under the scheduler-v2 engine so a lone agent reliably picks up available work.
- Fixed stuck jobs that could hang at "completing" instead of finishing.

## 2.1.0 - 2026-06-05

The scheduler-v2 rewrite: a ground-up redesign of how jobs are dispatched and tracked.

- Rewritten scheduling engine with more reliable new-job dispatch across the agent fleet.
- More accurate keyspace tracking and progress reporting, including corrections for how estimated keyspace transitions to actual values.
- Boot-time job keyspace repair: jobs with inconsistent keyspace state are reconciled on server startup.

## 2.0.0 - 2026-03-27

A major milestone that consolidates the entire 1.x series (v1.0.1 through v1.5.0) into a stable 2.0 baseline. Highlights delivered across the 1.x line:

- **Multi-Team Dynamics** — team-based access control, agent visibility, and resource isolation.
- **SSO Authentication** — enterprise LDAP, SAML 2.0, and OAuth/OIDC integration.
- **Priority-Based Scheduling** — intelligent agent allocation with configurable overflow modes (FIFO / round-robin) and parallel task assignment.
- **Password Analytics** — a 13-section analysis engine with domain filtering and Windows/LM/NTLM hash linking.
- **Passkey / WebAuthn MFA** — passwordless authentication.
- **Internationalization** — the UI in 6 languages.
- **Docker Agent** — containerized agent deployment with GPU passthrough.
- **User API** — a complete REST API with API-key authentication.
- **Certbot / ACME SSL** — automated certificate management.

The detailed 1.x releases that make up this baseline are below.

## 1.5.0 - 2026-03-27

- **Multi-team access control** — isolate clients, hashlists, and jobs by team, gated by a `teams_enabled` toggle that preserves single-team behavior when off. Access flows Client → Hashlist → Job, with trust-based agent scheduling and team-filtered views across the app.
- **Notification system** — multi-channel (in-app, email, webhook) notifications with 11 event types, a real-time notification bell, Discord/Slack/Teams webhook auto-detection, per-user preferences, and agent-offline monitoring.
- **Internationalization** — UI available in 6 languages.
- **Custom hashcat charsets** and custom argument support.
- **Job analytics dashboard.**
- Significant SSL/TLS, potfile management, and scheduling reliability improvements.

## 1.4.0 - 2026-01-17

*(includes 1.4.1–1.4.3)*

- **SSO authentication** — LDAP direct-bind, SAML 2.0 (AuthnRequest/ACS/metadata, auto-generated SP keys, request signing, HTTP-Redirect + HTTP-POST bindings), and OAuth 2.0 / OIDC (authorization-code flow with PKCE, ID-token validation).
- AES-256-GCM encryption for sensitive secrets (bind passwords, private keys, client secrets).
- Just-In-Time user provisioning with an admin approval workflow, account linking by email, and per-user authentication overrides.
- **Association attacks** support.
- **Admin diagnostics system** (1.4.1) and admin settings fixes (1.4.3).
- User soft-delete endpoint and SSO audit-trail improvements.

## 1.3.0 - 2025-12-01

*(includes 1.3.1–1.3.7)*

- **Hashcat increment mode** — full support for `--increment` / `--increment-inverse` mask attacks, decomposed into per-length layers scheduled independently across agents (no agent changes required).
- **Complete User API** for programmatic access.
- Scheduling performance, agent stability, and crack-processing reliability improvements for high-volume environments.
- Patch releases added custom binary upload support, job-completion stability, analytics fixes, and fresh-install migration fixes.

## 1.2.0 - 2025-10-23

- Password analytics with domain filtering.
- Enhanced hash processing and security improvements.

## 1.1.0 - 2025-10-12

- **Accurate keyspace tracking** from hashcat progress values, replacing estimates with real captured values.
- Agent download UX improvements and Docker deployment enhancements.
- **Breaking:** agent-backend WebSocket protocol change — agents must update to 1.1.0.

## 1.0.0 - 2025-10-07

First stable release.

- Full hashcat coverage (589 hash types).
- Distributed password-cracking coordination with multi-agent resource management.
- REST API with JWT authentication and MFA (TOTP, email, backup codes).
- Real-time job progress tracking.
- Role-based access control.

## Pre-1.0 (2025-01 – 2025-09)

The 0.x development line that led to the first stable release. Key milestones across the 0.9–0.16 series: initial Docker containerization; the potfile system; job-naming enhancements; message buffering and reconnection resilience; intelligent job interruption and priority-override scheduling; MFA and user-management fixes; job-completion notifications and hash-type management; memory-efficient streaming file uploads (FormStream); keyspace chunking and continuation; and agent file-sync reliability. See the [GitHub Releases page](https://github.com/ZerkerEOD/krakenhashes/releases) for the full per-tag detail.

<!--
Issue / PR reference links. Cite the number in an entry as a reference-style
link, e.g. "([#40])", and add its definition below. GitHub redirects
/issues/N to /pull/N (and vice versa), so one /issues/N URL resolves whether
N is an issue or a PR — no need to know which it is up front.
-->

[#40]: https://github.com/ZerkerEOD/krakenhashes/issues/40
