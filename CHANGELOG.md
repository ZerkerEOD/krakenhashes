# Changelog

All notable changes to KrakenHashes, newest release first. This file tracks tagged
releases; the complete per-tag history is available on the [GitHub Releases page](https://github.com/ZerkerEOD/krakenhashes/releases).
Patch releases are rolled into their minor version's section.

## 3.0.0 - 2026-10-23

The second major milestone. Everything delivered since 2.1.2 (roughly 50 pull requests): on-demand cloud GPUs, agents that update themselves, a rebuilt interface in 17 languages, and a lot of hardening driven by issues people filed while running it for real.

### Highlights

- **Cloud GPU provisioning** — when your own agents are saturated, a job can rent GPUs on demand and give them back when it is done. Spend is budgeted per client and reserved up front, instance lifetime is sized to the remaining work, and several independent paths guarantee teardown (including a watchdog inside the instance). AWS EC2 is tested end to end; RunPod and Vast.ai are built but marked **experimental**. Cloud agents must join over a VPN — there is no public-internet mode ([#94], [#107]).
- **Agent auto-update** — a new launcher supervises the agent and swaps its binary when the server is upgraded: download, checksum, health-check and automatic rollback, and only while the agent is idle so a running job is never interrupted. Per-user service install on Linux, macOS and Windows ([#46]).
- **Agent mTLS and revoke** — agents authenticate with a per-agent certificate on the existing port, migrating automatically with no new port or firewall change. A new Revoke action is the hard kill (invalidates the credential and cuts the connection) ([#104]).
- **Network-share storage** — wordlists and rules can live on an NFS/SMB share, with a per-agent storage tier (full cache, on-demand LRU, or direct read-only mount) and a resumable, MD5-checked migration between local and share storage ([#83], [#101]).
- **Loopback attacks** — re-run a step's rules or hybrid mask against only the newly cracked passwords, round after round, until a round finds nothing new ([#64]).
- **Analytics API, BloodHound enrichment, and PDF export** — analytics over the user API, optional in-memory BloodHound AD-privilege enrichment (never written to disk), and internal (full) vs external (server-redacted) PDF reports ([#66]).
- **Rebuilt frontend** — one shared table component, light and dark themes, a grouped sidebar, an admin settings hub with autosave, a client detail page, and a dashboard scoped to you or your teams. UI now in **17 languages** (up from 7) ([#109]).
- **Custom branding** — set the app name, colours, logo and favicon, applied to the login page, PDFs, emails, the TOTP issuer and webhooks. "Powered by KrakenHashes" attribution is enforced server-side ([#41]).
- **Multi-Team hardening** — closed a cross-team IDOR on the hashlist download endpoints and added team-access checks to 37 handlers that were missing them ([#100], [#48]).
- **Scheduler improvements** — automatic slow-hash candidate mode (`-S`) keeps the GPU fed on bcrypt/PBKDF2/WPA-style hashes under rules, a chunk-overrun guard stops runaway tasks, and admins can set a default and enforced ceiling for max agents per job ([#70], [#126]).

### Upgrade notes (from 2.x)

Read the full [upgrade guide](https://zerkereod.github.io/krakenhashes/latest/deployment/updates/#upgrading-to-300-from-2x) before you start. In short:

- **Back up the database first.** Some migrations rewrite data, one of them (`$HEX[...]` password decoding) cannot be reversed, and two take a write lock on the `hashes` table while they run (an index rebuild and a `cracked_at` backfill) — plan for minutes on large tables and `VACUUM ANALYZE hashes` afterwards. Rolling back means restoring the backup.
- **Existing 2.x agents keep working** on their API key and are flagged "Update available"; move them onto the launcher (or replace the binary) to get mTLS and auto-update. New installs should use a 3.x agent. Port 1337 is still needed for agents (CA download and certificate renewal).
- **Removed or changed:** rule splitting and its settings; the agent port-swap/auth-mode settings; `GET /api/dashboard` (replaced by `/api/dashboard/*`). Custom job creation now returns `202 preparing`; stricter multi-team checks return 404 for out-of-team resources; "Remember Me" now only pre-fills the username.

### Agent and scheduling

- Launcher-based auto-update pipeline with rollout limits, retry and rollback ([#46]).
- Slow-hash candidate mode (`-S`) selected automatically for slow hashes combined with rules or a hybrid mask ([#70]).
- Chunk-overrun guard, stable PCI-based device identity, and a surgical `--force` autotune retry when hashcat skips kernel autotune.
- Loopback re-runs, with per-step and per-job toggles and a monitoring panel; team-scoped and leak-fixed ([#64], [#78], [#79]).
- Admin default and enforced ceiling for a job's max agents ([#126]).
- Hybrid job naming and a Hashlist/Preset column in the jobs list ([#49]).
- Removed rule splitting in favour of keyspace splitting, and consolidated the keyspace multiplier.
- Many keyspace and progress fixes: premature completion, chunk-progress display, salted jobs stuck at 99.99%, keyspace pre-flight no longer failing job creation ([#62], [#76], [#77], [#84]).
- Per-job rule sync and MD5 verification at dispatch, client-specific wordlist/potfile delivery, and automatic recovery of files missing on disk ([#61], [#67], [#93]).

### Data and reporting

- Analytics and reporting over the user API ([#66]).
- Optional BloodHound AD-privilege enrichment, parsed in memory and never persisted ([#66]).
- Internal and external (server-redacted) PDF export, plus a domain-extraction fix.
- Long Kerberos hashes (13100) can now be imported; usernames and realms are extracted for 13100/19600/19700 ([#88]).
- The Cracked Hashes list is sorted by crack time with a new `cracked_at` column, and real passwords are stored instead of `$HEX[...]` literals ([#90], [#112]).
- Custom hashcat charsets, HEX charsets and per-job custom arguments ([#32], [#35], [#44]).

### Security

- Fixed a cross-team IDOR on the hashlist hashes/download endpoints and added team-access checks to 37 handlers ([#100], [#48]).
- Scoped the hashlist client picker and job lists to the caller's teams ([#51]).
- Dependency CVE updates, a SAML decompression-bomb cap, upload-metadata and email-header hardening, and the Vite migration clearing the frontend's npm advisories.
- Auth determinism fixes: duplicate-JWT collision and first-time authenticator-app setup ([#89]).

### Frontend, branding and interface

- Full redesign with light/dark themes and a team-scoped dashboard, and 10 new UI languages for 17 total ([#109]).
- Custom branding with server-enforced attribution ([#41]).
- Dashboard and view fixes: inflated/again-non-zero agent hashrate, cross-team job-name leak, empty hashlist view, and the team filter on the hashlist list ([#111], [#113], [#114]).
- Reworded the credential-stuffing use case ([#108]).

### Thanks

A lot of this release came straight out of issues people filed while using KrakenHashes on real work. Thank you to [@Lat3magnet1c], [@Lolozendev], [@Devil123166], [@LorisP-ISEC], [@0xDECEA5ED], [@clem9669] and [@mjk72578] for the reports, feature requests and the security disclosure.

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

[#32]: https://github.com/ZerkerEOD/krakenhashes/issues/32
[#35]: https://github.com/ZerkerEOD/krakenhashes/issues/35
[#40]: https://github.com/ZerkerEOD/krakenhashes/issues/40
[#41]: https://github.com/ZerkerEOD/krakenhashes/issues/41
[#44]: https://github.com/ZerkerEOD/krakenhashes/issues/44
[#46]: https://github.com/ZerkerEOD/krakenhashes/issues/46
[#48]: https://github.com/ZerkerEOD/krakenhashes/issues/48
[#49]: https://github.com/ZerkerEOD/krakenhashes/issues/49
[#51]: https://github.com/ZerkerEOD/krakenhashes/issues/51
[#61]: https://github.com/ZerkerEOD/krakenhashes/issues/61
[#62]: https://github.com/ZerkerEOD/krakenhashes/issues/62
[#64]: https://github.com/ZerkerEOD/krakenhashes/issues/64
[#66]: https://github.com/ZerkerEOD/krakenhashes/issues/66
[#67]: https://github.com/ZerkerEOD/krakenhashes/issues/67
[#70]: https://github.com/ZerkerEOD/krakenhashes/issues/70
[#76]: https://github.com/ZerkerEOD/krakenhashes/issues/76
[#77]: https://github.com/ZerkerEOD/krakenhashes/issues/77
[#78]: https://github.com/ZerkerEOD/krakenhashes/issues/78
[#79]: https://github.com/ZerkerEOD/krakenhashes/issues/79
[#83]: https://github.com/ZerkerEOD/krakenhashes/issues/83
[#84]: https://github.com/ZerkerEOD/krakenhashes/issues/84
[#88]: https://github.com/ZerkerEOD/krakenhashes/issues/88
[#89]: https://github.com/ZerkerEOD/krakenhashes/issues/89
[#90]: https://github.com/ZerkerEOD/krakenhashes/issues/90
[#93]: https://github.com/ZerkerEOD/krakenhashes/issues/93
[#94]: https://github.com/ZerkerEOD/krakenhashes/issues/94
[#100]: https://github.com/ZerkerEOD/krakenhashes/issues/100
[#101]: https://github.com/ZerkerEOD/krakenhashes/issues/101
[#104]: https://github.com/ZerkerEOD/krakenhashes/issues/104
[#107]: https://github.com/ZerkerEOD/krakenhashes/issues/107
[#108]: https://github.com/ZerkerEOD/krakenhashes/issues/108
[#109]: https://github.com/ZerkerEOD/krakenhashes/issues/109
[#111]: https://github.com/ZerkerEOD/krakenhashes/issues/111
[#112]: https://github.com/ZerkerEOD/krakenhashes/issues/112
[#113]: https://github.com/ZerkerEOD/krakenhashes/issues/113
[#114]: https://github.com/ZerkerEOD/krakenhashes/issues/114
[#126]: https://github.com/ZerkerEOD/krakenhashes/issues/126
[@Lat3magnet1c]: https://github.com/Lat3magnet1c
[@Lolozendev]: https://github.com/Lolozendev
[@Devil123166]: https://github.com/Devil123166
[@LorisP-ISEC]: https://github.com/LorisP-ISEC
[@0xDECEA5ED]: https://github.com/0xDECEA5ED
[@clem9669]: https://github.com/clem9669
[@mjk72578]: https://github.com/mjk72578
