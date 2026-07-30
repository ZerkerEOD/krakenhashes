# BloodHound AD-Privilege Enrichment

## Overview

BloodHound enrichment cross-references an analytics report's **cracked** accounts against an uploaded
Active Directory collection dump (SharpHound `.zip` or BloodHound `.json`) to answer "which of the
credentials we recovered actually confer privilege?" — effective Domain Admin, tier-0 membership,
Kerberoastable/AS-REP-roastable exposure, `adminCount` protection, DCSync rights, local-admin blast
radius, and a graph **attack path to Domain Admin**. It turns "we cracked 4,000 hashes" into "we
cracked 3 of 5 Domain Admins and 18 accounts with a path to DA".

Enrichment is entirely **opt-in per report**: the new sections appear only when a dump is uploaded
with the report; a normal report is unaffected.

!!! danger "Hard constraint: the raw dump is never persisted"
    The uploaded collection dump is parsed **in memory only** and is **never written to disk**. Only a
    compact, plaintext-free *derived* per-account fact set is staged in the database, and that derived
    context is **auto-deleted once the report finishes generating**. Re-analysis requires re-uploading
    the dump. Every design decision below serves this constraint.

## Data flow

```
POST /api/analytics/reports/bloodhound        (JWT app API)      multipart/form-data
POST /api/v1/analytics/reports/bloodhound      (API-key user API) multipart/form-data
   │  http.MaxBytesReader caps the request body (limits.go)
   │  formstream streams the parts; the file part goes straight to the parser
   │  (never os.Create / os.CreateTemp), metadata parts read as form fields
   ▼
 in-memory:  ParseInput → Collector (membership + attack graph) → Resolve → DerivedContext (compact)
   │  the raw graph is discarded when the request handler returns
   ▼
 SINGLE ATOMIC INSERT:  analytics_reports row (status=queued) + bloodhound_context JSONB
   ▼
 AnalyticsQueueService poller (existing, ~10s, one-at-a-time) → AnalyticsService.GenerateAnalytics
   │  loads bloodhound_context → enrichReportWithBloodhound() → sets the *omitempty AD sections
   │  UpdateAnalyticsData (enriched)
   ▼
 ClearBloodhoundContext (SET NULL)          ← derived facts gone once the report has run
   + TTL sweep backstop                       ← guarantees deletion even on crash / missed clear
```

The single atomic INSERT of **row + context together** is the linchpin: the async queue never observes
a queued BloodHound report without its context, so there is no "create then attach" race, and the
never-persist guarantee survives a server restart between upload and generation.

## In-memory parsing (`internal/services/bloodhound/decode.go`)

`ParseInput(r io.Reader, filename string, coll *Collector)` handles both container shapes without ever
touching disk:

- **Single `.json`** streams straight into a token-level `json.Decoder`. The `data` array is consumed
  element-by-element so the whole file is never materialized, and the decoder tolerates `meta` appearing
  either before or after `data`. Object type is resolved from `meta.type`, then a filename hint, then a
  per-element sniff.
- **`.zip`** is read through an `io.LimitReader` into a bounded `bytes.Buffer`, then opened with
  `zip.NewReader(bytes.NewReader(...), size)` (archive/zip needs random access). Each entry is streamed
  through its own per-entry `LimitReader`.

The multipart plumbing lives in `internal/handlers/analytics/bhupload/bhupload.go` (`Parse`), shared by
both the JWT handler (`handlers/analytics/handler_bloodhound.go`) and the API-key handler
(`handlers/api/v1/analytics_handler.go`). The invariant — **no `os.Create`/`os.CreateTemp`/`os.MkdirAll`
anywhere in the package** — is documented at the package top and is the thing to guard in review.

## Graph build (`internal/services/bloodhound/graph.go`)

The `Collector` accumulates two graphs incrementally while the dump streams, then discards them after
`Resolve()`:

- **Membership graph** — `groups[].Members[]` → `member --MemberOf--> group`; `PrimaryGroupSID` edges.
  Transitive group→group closure is memoized and cycle-safe (`groupClosure`).
- **Attack graph (reverse adjacency)** — local-admin / RDP / PSRemote / DCOM edges from
  `computers[]`, control ACEs (`GenericAll`, `WriteDacl`, `WriteOwner`, `ForceChangePassword`,
  `AddMember`, `ReadGMSAPassword`, `AddKeyCredentialLink`, …), delegation edges, and domain replication
  (DCSync) rights folded into a per-principal bitset.

**Duplicate-SID de-collision:** the graph is keyed by `ObjectIdentifier` (SID). Malformed, merged, or
synthetic dumps can carry the *same* SID on two different accounts (real AD never reuses a live RID).
`addUser` never lets a **disabled** record clobber an **enabled** one, so a live, possibly-privileged
account (e.g. a cracked gMSA) is not silently dropped in favor of a tombstoned namesake. Enabled wins
regardless of file order; same-enabled keeps last-seen.

## Privilege resolution (`resolve.go`, `paths.go`)

For each **in-scope** account (SID present in the report's hashlists), `factsFor` computes, over the
transitive effective-group set:

- **effectiveDomainAdmin** — membership resolving to RID 512/519 or BUILTIN Administrators
  (`S-1-5-32-544`, incl. the domain-prefixed BHCE form).
- **isTierZero** — own high-value flag or any effective group flagged privileged/tier-0.
- **dcsync** — self or any effective group holds GetChanges **and** GetChangesAll on a domain.
- **localAdminCount** — distinct computers admin-reachable by the account or its groups.
- Node flags straight from `Properties`: enabled, adminCount, hasSPN, dontReqPreauth, sensitive,
  unconstrained delegation.

**Path-to-DA** is a single reverse BFS from the target set (Domain Admins, Enterprise Admins,
Administrators, the domain object, Domain Controllers, any tier-0 node) over the reversed attack graph:
one O(V+E) pass yields, for every account, whether it can reach a target and its hop distance — not a
per-source search. If the graph exceeds the edge/object budget, path analysis is skipped
(`PathToDAStats.skipped = true`) and membership/DCSync/local-admin facts are still emitted (graceful
degradation).

## The DerivedContext — the only thing persisted (`context.go`)

`Resolve()` produces a compact `DerivedContext`: per-in-scope-account `AccountFacts` (SID + the resolved
booleans/counts + privileged-group names, **no plaintext, no ACE detail**), per-domain denominators
(so a report can say "3 of 5 Domain Admins in the domain"), and lookup indexes. It is persisted as the
`analytics_reports.bloodhound_context` JSONB column
(migration `20260728120000_add_bloodhound_context`).

**Matching** (`DerivedContext.Lookup`) resolves a cracked `(username, domain)` to its facts,
case-insensitively and in layers: an embedded `DOMAIN\` prefix, then the provided domain (FQDN or
NetBIOS first label via an alt index), then a globally-unique sAMAccountName fallback. A trailing `$`
(machine / gMSA accounts) is normalized consistently across build, in-scope seeding, and lookup so those
accounts match whether or not the recovered credential kept the `$`. Ambiguous sAMAccountNames are
deliberately omitted from the unique-sam index — a miss is safer than a mis-attribution.

## Enrichment + never-persist lifecycle

- `internal/services/analytics_service_bloodhound.go` — `enrichReportWithBloodhound` loads the context,
  fetches the report's cracked account refs, and `enrichAnalyticsWithBloodhound` cross-references them
  (deduped by SID) into the `*omitempty` sections on `AnalyticsData` plus high-severity recommendations
  (CRITICAL when any cracked account is effective-DA, holds DCSync, or has a path to DA).
- `internal/services/analytics_service.go` — `GenerateAnalytics` calls the enrichment after building the
  base analytics, then, on the completed path, `ClearBloodhoundContext` (SET NULL). The non-BloodHound
  path is unchanged (a nil context short-circuits).
- `internal/repository/analytics_bloodhound_repository.go` — `CreateWithBloodhound` (the atomic INSERT),
  `GetBloodhoundContext`, `ClearBloodhoundContext`, and `SweepStaleBloodhoundContexts`. The context is
  never included in the general report SELECTs and the model field is tagged `json:"-"`, so no analytics
  read path can leak it.
- `internal/services/analytics_queue_service.go` — a **TTL sweep** backstop clears any context left
  behind by a crash or a missed clear, so the never-persist guarantee holds even on the failure path.

## Redaction (`internal/services/analytics/pdf/redact.go`)

`redactBloodhound` strips every per-account leaf (`accounts` / `top_accounts` — usernames, SIDs, group
names) from the **external** (client-facing) PDF while keeping the aggregate scalars (counts,
percentages, computer-exposure totals). Internal PDFs render the full per-account detail. This reuses
the existing fail-closed JSON-round-trip deep copy, so the cached report struct is never mutated.

## Limits (`limits.go`)

`MaxRequestBytes` (via `http.MaxBytesReader`), `MaxZipBytes` (buffered-zip cap), `MaxEntryBytes` +
`MaxTotalDecompressed` (zip-bomb guard measured from real bytes read, never trusting a header's
`UncompressedSize64`), `MaxObjects` / `MaxEdges` (graph caps → skip path-finding past them), and
`MaxUsersForDomainAgg` (skip closure-based denominators past it). An oversized or bomb dump is rejected
cleanly with no disk write and no OOM.

## Key files

| File | Responsibility |
|------|----------------|
| `internal/services/bloodhound/decode.go` | In-memory zip/json streaming parse |
| `internal/services/bloodhound/graph.go` | Collector: membership + attack graph, de-collision |
| `internal/services/bloodhound/resolve.go` | Per-account privilege resolution → DerivedContext |
| `internal/services/bloodhound/paths.go` | Attack-edge model + reverse-BFS path-to-DA |
| `internal/services/bloodhound/context.go` | DerivedContext + cracked-account matching |
| `internal/services/bloodhound/limits.go` | Size/count/edge caps + zip-bomb guards |
| `internal/handlers/analytics/bhupload/bhupload.go` | Shared multipart (formstream) upload parse |
| `internal/repository/analytics_bloodhound_repository.go` | Atomic INSERT, get/clear/sweep context |
| `internal/services/analytics_service_bloodhound.go` | Cross-reference cracked accounts → sections |
| `internal/services/analytics/pdf/redact.go` | External-export account-leaf redaction |
| `db/migrations/20260728120000_add_bloodhound_context.*` | `bloodhound_context` JSONB column |

## Related

- [Analytics Reports](../../user-guide/analytics-reports.md) — the user-facing feature guide.
- User API: `docs/user-api/USER_GUIDE.md` (Workflow 5) and `openapi.yaml` — the programmatic surface.
