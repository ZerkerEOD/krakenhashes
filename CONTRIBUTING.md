# Contributing to KrakenHashes

Thanks for your interest in contributing! KrakenHashes is an open-source, distributed
password-cracking platform for security professionals. Contributions from the community —
bug fixes, features, docs, and translations — are very welcome.

**AI-assisted contributions are welcome.** What we care about is that the code is correct,
follows the conventions below, and is something you understand and can stand behind. Please
don't open low-effort or unreviewed machine-generated PRs — those waste maintainer time and
may be flagged and closed.

Every pull request is automatically reviewed by [CodeRabbit](https://coderabbit.ai). It posts
a summary and inline review comments, checks that user-facing changes update the docs, and
notes when UI text needs translations. A human maintainer always makes the final merge
decision — CodeRabbit never merges or approves on its own.

---

## Project layout

| Directory   | Component | Stack |
|-------------|-----------|-------|
| `backend/`  | REST API + job scheduling | Go (`github.com/ZerkerEOD/krakenhashes/backend`) |
| `agent/`    | Distributed hashcat execution | Go (`github.com/ZerkerEOD/krakenhashes/agent`) |
| `frontend/` | Web UI | React 18 + TypeScript + Material-UI |
| `docs/`     | User & reference docs | Markdown (MkDocs) |

## Getting set up

### Backend (Docker)

The backend is developed and tested through Docker — please don't run `go build` directly in
`backend/` (it drops binaries into the source tree). Always use the dev compose file:

```bash
docker-compose down
docker-compose -f docker-compose.dev-local.yml up -d --build
# logs
docker-compose -f docker-compose.dev-local.yml logs -f backend
```

> ⚠️ Never run `docker-compose down -v` — the `-v` flag deletes Docker volumes, including your
> PostgreSQL data. Use `docker-compose down` without `-v`.

Run Go tests with the backend Makefile:

```bash
cd backend
make test            # all tests
make test-unit       # unit tests only
make test-integration
```

### Agent (Makefile)

```bash
cd agent
make clean && make build      # current platform
make clean && make build-all  # all platforms (output in ../bin/agent/)
make test
make lint && make vet && make fmt
```

You can run an agent without GPU hardware using mock mode (`--test-mode`) for testing the
scheduler — see `CLAUDE.md`/`README.md` for the mock-agent environment variables.

### Frontend (npm)

```bash
cd frontend
npm install
npm start            # dev server on :3000
npm run build
npm test
```

## Commit & PR conventions

- **Use [Conventional Commits](https://www.conventionalcommits.org/):**
  `type(scope): summary` — e.g. `feat(backend): add agent overflow allocation mode`.
  Types: `feat | fix | docs | chore | refactor | test | perf`.
  Scopes: `backend | frontend | agent | docs | db`.
- **Do not add `Co-Authored-By` trailers** to commits.
- Keep PRs focused; fill out the pull-request template (what/why, components touched, how you
  tested). Link issues with `Closes #123`.
- Never commit secrets, credentials, real hashes, or large data files.

## Code conventions

These are the conventions CodeRabbit and maintainers review against:

**Backend (Go)**
- Repository pattern: all DB access goes through repositories using the `*db.DB` wrapper and
  standard `database/sql` (`Query`/`QueryRow`/`Exec` with manual `rows.Scan`) — not sqlx struct
  tags. Transactions use `db.Begin()`.
- Business logic lives in the service layer, not in HTTP handlers.
- Router is gorilla/mux (`mux.Vars(r)`). Add JWT + role authorization to every new endpoint.
- The model type is `HashList` (capital `L`).

**Agent (Go)**
- Backend communication is WebSocket with a heartbeat; messages are JSON routed by a `type`
  field.
- Cross-platform (Linux/Windows/macOS) — keep build-tagged pairs (`*_unix.go` / `*_windows.go`)
  in sync and avoid OS-specific assumptions.

**Frontend (React/TS)**
- Pages use `<Box sx={{ p: 3 }}>` for layout, never `Container` with `maxWidth`.
- Data fetching uses React Query; API calls go through the services layer
  (`services/api.ts`), not direct `fetch`/`axios` in components.
- Auth state comes from context (the field is `isAuth`).
- Management page titles are singular ("Agent Management").

## Database migrations

- Migrations live in `backend/db/migrations/` and are applied automatically on startup in
  Docker.
- Every migration must include **both** an `*.up.sql` and a matching `*.down.sql`, and the down
  must cleanly reverse the up.
- Never modify an already-released migration — add a new one.

## Documentation (required for user-facing changes)

The docs site is built from the `docs/` tree. **If your PR changes user-facing behavior** — a
new or changed API endpoint/field, configuration or environment variable, CLI flag, agent
behavior, install/setup step, or a visible frontend feature — **update `docs/` in the same
PR.** CodeRabbit enforces this as a pre-merge check; a maintainer can waive it with the
`no-docs-needed` label for refactors, test-only changes, dependency bumps, and typo fixes.

## Translations (i18n)

The frontend uses [react-i18next](https://react.i18next.com/); strings live in
`frontend/public/locales/<lang>/*.json`, with **`en/` as the reference language**.

- User-facing text must go through i18n — don't hardcode strings in components.
- When you add or change UI text, add/update the keys in `frontend/public/locales/en/*.json`.
- You don't need to provide other languages (de, es, …) in your PR — those are translated
  afterward. CodeRabbit posts a non-blocking note when translations look out of date.

See [`docs/contributing/translations.md`](docs/contributing/translations.md) for the full
translation guide.

## License

By contributing, you agree that your contributions are licensed under the project's
[GNU AGPL-3.0](LICENSE) license.
