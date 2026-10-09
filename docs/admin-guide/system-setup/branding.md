# Application Branding

KrakenHashes can be presented under your organisation's name, colours and logo so it fits
alongside your other internal tooling. Branding is configured by an administrator in
**Admin → Settings → System Settings → Branding** (a section at the bottom of the System Settings
tab) and applies instantly to every user.

## What can be customised

| Setting | Where it appears | Default |
|---|---|---|
| **Application name** | Header, login page, About page, notification emails (`{{ .AppName }}`), webhook payloads, new authenticator (TOTP) enrolments, analytics PDF cover and running header | KrakenHashes |
| **Browser page title** | Browser tab / window title | Application name |
| **Primary accent colour** | Buttons, links, highlights; rules, bars and section accents in analytics PDFs | `#ff0000` |
| **Secondary accent colour** | Material-UI secondary palette; subtitle rules in analytics PDFs | none |
| **Logo** | Header, login page, analytics PDF cover and running header | Kraken emblem |
| **Favicon** | Browser tab icon | Kraken favicon |

The dark background surfaces are fixed brand values and are not configurable. Pick accent colours
that stay readable on near-black surfaces.

## Attribution policy

The **"powered by KrakenHashes"** attribution is mandatory and is enforced by the server:

- With a custom name or title, the browser title is `<your title> · powered by KrakenHashes`. The
  suffix is appended on every read and stripped from whatever is typed into the title field, so it
  cannot be removed or doubled. With no custom name or title the browser title is simply
  `KrakenHashes`.
- The header, login page and footer show the attribution line whenever a custom name or logo is set.
- Every analytics PDF keeps the Kraken emblem and "powered by KrakenHashes" in the page footer,
  regardless of the logo and name shown on the cover.
- The public agent-download page and the webhook test message carry the same line.

## Image requirements

| Asset | Formats | Limits |
|---|---|---|
| Logo | PNG, JPEG | 2 MB, 2048 × 2048 px |
| Favicon | PNG, ICO | 512 KB, 512 × 512 px |

SVG and other formats are rejected on purpose: served inline, an SVG can carry scripts. Uploaded
files are validated by content (not by extension), renamed to a generated name and stored in
`<KH_DATA_DIR>/branding/`. Replacing or removing an image deletes the previous file.

Browsers cache favicons aggressively. The icon URL carries a version query that changes on every
upload, but a hard reload may still be needed once.

## Analytics PDF exports

When branding is configured, both the internal and the external PDF use the organisation name and
logo on the cover and in the running header, the primary colour for the accent rule, section
accents and bar charts, and the secondary colour for subtitle rules. Classification banners and
severity colours never change. The built-in PDF fonts cover Latin-1 only, so an organisation name
with characters outside that range renders with substitutions.

## API

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/api/branding` | none | Effective branding for the UI (name, title, colours, asset URLs) |
| `GET` | `/api/branding/logo`, `/api/branding/favicon` | none | The uploaded image, or `404` when none is set |
| `GET` / `PUT` | `/api/admin/settings/branding` | admin | Read / update the text settings |
| `POST` / `DELETE` | `/api/admin/settings/branding/logo`, `…/favicon` | admin | Upload (multipart field `file`) / remove an image |

The public endpoints are unauthenticated so the login page can render the configured identity
before sign-in. They expose only the branding itself, never other settings.

## Storage

Text settings live in `system_settings` under `branding_*` keys. They are seeded as `NULL`
("not configured") and the generic `PUT /api/admin/settings/{key}` route refuses to write them;
use the branding endpoints above, which validate the values.
