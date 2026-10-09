# OzDDNS

A **provider-centric, pluggable, single-binary dynamic DNS manager** with a web panel
and a CLI that share the same business logic.

> Reconciliation, not polling: for every task OzDDNS reads the record that exists, reads
> the address the host holds, and writes **only** when they disagree — and shows you the
> diff when it does.

One Go binary (`ozddns`) contains three things:

1. **`ozddns serve`** — a REST API plus an embedded web panel (`go:embed`).
2. **`ozddns <command>`** — a management CLI that calls the *same* application layer.
3. **The engine** — a scheduler + reconciler shared by both.

---

## Quick start

```bash
# 1. Build
make build                     # → ./bin/ozddns

# 2. Run with the demo dataset (providers, tasks and history to look at)
./bin/ozddns --db ./ozddns.db serve --demo
#   OzDDNS v1.0.0 — panel on http://127.0.0.1:9876/

# 3. Or drive it from the CLI
./bin/ozddns --db ./ozddns.db provider add --type cloudflare --name "Home CF" --set api_token=… --set proxied=true
./bin/ozddns --db ./ozddns.db source   add --name "ipify" --type http --family v4 --target https://api.ipify.org
./bin/ozddns --db ./ozddns.db job      add --name "Home IPv4" --provider "Home CF" --source ipify --target home.example.com:A:300
./bin/ozddns --db ./ozddns.db job      run --dry-run <job-id>   # show the diff, write nothing
./bin/ozddns --db ./ozddns.db job      run <job-id>             # reconcile for real
./bin/ozddns --db ./ozddns.db run      list
./bin/ozddns --db ./ozddns.db status
```

Docker:

```bash
docker compose up -d --build     # panel on http://127.0.0.1:9876 (loopback-published)
```

---

## Repository layout

```
ozddns/
├── cmd/ozddns/main.go          # the single binary entry point
├── internal/
│   ├── domain/                 # entities shared by every layer (no deps)
│   ├── provider/               # Provider/Factory contract, registry, HTTP + zone helpers
│   │   ├── cloudflare/ alidns/ dnspod/ duckdns/ namecheap/
│   │   └── godaddy/ desec/ vercel/ custom/
│   ├── providers/all.go        # one Register line per adapter — no central switch
│   ├── ipsource/               # pluggable IP sources: http · interface · command · static
│   ├── engine/                 # reconciler (diff + write) and scheduler
│   ├── storage/                # repository interfaces + sqlite implementation
│   ├── app/                    # use-cases shared by the API and the CLI
│   ├── server/                 # REST handlers + middleware (auth, logging, recovery)
│   ├── seed/                   # first-run defaults and the demo dataset
│   ├── cli/                    # command-line interface
│   └── version/                # build metadata (-ldflags)
├── web/                        # the panel (embedded): index.html + assets/
│   └── screens/                # reference screenshots
├── docs/
│   ├── 01-design.md            # design system, IA, screens, flows, API contract
│   ├── 02-cases-and-testdata.md# every case + the canonical seed data
│   ├── 03-reference-analysis.md# ddns-updater & ddns-go and how OzDDNS differs
│   └── 04-delivery.md          # how to run, CLI/API reference, case→test map
├── Makefile · Dockerfile · compose.yaml
```

## The panel

The panel is a dependency-free SPA (classic scripts, hash routing) served from the
binary. On load it calls `GET /api/v1/bootstrap` and **binds to the live backend**;
if that call fails (e.g. you opened `web/index.html` from disk) it falls back to the
built-in mock dataset, so the design prototype still works offline.

Screens: **Overview**, **Providers** (+ detail: Overview / Configuration / Used by),
**Sync tasks** (+ the 5-step New-task wizard + reconciliation detail), **Executions**
(+ run drawer with the old→new diff and provider exchange), **Settings** (General /
Security / IP sources / Notifications / Storage / About) and **Sign in**.

Example deep links: `#/providers`, `#/providers/p-cf/configuration`, `#/jobs/j-nas`,
`#/jobs/new`, `#/executions`, `#/settings/security`.

Screenshots live in `web/screens/`.

## Languages

The panel ships with **English, 简体中文 and 日本語** and switches instantly. A
language is chosen from **Settings → General → Language** (persisted server-side in
`settings.language`), or per-open with a query string — handy for demos and for
sharing a link in one language:

```
http://127.0.0.1:9876/?lang=zh-CN#/overview
http://127.0.0.1:9876/?lang=ja#/providers
```

`web/assets/i18n.js` holds the engine and both catalogs. It works two ways:

1. `OZ.t('Enabled {name}', { name })` for strings assembled in JavaScript
   (toasts, counters, relative times) — see `OZ.t` / `OZ.i18n.t`.
2. A render-time DOM pass (`OZ.i18n.applyDOM`) that swaps every **whole** static
   text node and `placeholder`/`title`/`aria-label` whose exact English phrase is
   in the catalog. Because the catalog is keyed by the English source, an
   untranslated phrase simply falls through — a missing entry can never break a
   screen.

To add a language: add a code + label in `i18n.js`, add a catalog object, and done.
Precedence at load: `?lang=` → saved choice (localStorage, offline) → server
`settings.language` → browser locale.

## Theme

Two palettes ship by default and switch instantly: **light** (ivory) and **dark**
(warm charcoal, not a generic black), plus an **auto** mode that follows the OS
via `prefers-color-scheme`. Flip it with the moon/sun button in the topbar or from
**Settings → General → Theme**; the choice is persisted server-side in
`settings.theme` and mirrored to `localStorage`. A query string pins a theme for
a session, mirroring `?lang=`:

```
http://127.0.0.1:9876/?theme=dark#/overview
```

`web/assets/theme.js` paints `data-theme` onto `<html>`; the palettes themselves
are tokens in `web/assets/tokens.css`.

## Design language

"Warm editorial": ivory paper, terracotta accent, serif display (Fraunces) + humanist
sans (Instrument Sans), hairline borders, generous whitespace. Tokens: `web/assets/tokens.css`;
rationale: `docs/01-design.md`.

## Adding a provider

There is **no central switch**. Write a package that implements `provider.Factory`
(`Meta`, `ValidateConfig`, `New`) and a `provider.Provider` (`Validate`, `ListZones`,
`ListRecords`, `Apply`), then add one line to `internal/providers/all.go`:

```go
r.Register(myprovider.Factory{})
```

The registry reflection feeds the panel's "Add provider" form automatically, and the
engine, API and CLI need no changes. See `internal/provider/custom/custom.go` for the
smallest complete example.

## Security

- **Loopback first**: the default listen address is `127.0.0.1:9876`. Binding `0.0.0.0`
  logs a warning; set a password before doing so.
- **Secrets are masked** on every read path (API + CLI); the panel never receives a
  plaintext credential it didn't just type.
- HTTP Basic auth is enforced on `/api/v1/*` once a password is set. `ozddns config export`
  never includes the password hash.

## Reference projects

The provider model is grounded in a source-level reading of two mature tools:

- [`qdm12/ddns-updater`](https://github.com/qdm12/ddns-updater) — one `Provider.Update`
  per provider, JSON config, ~60 providers.
- [`jeessy2/ddns-go`](https://github.com/jeessy2/ddns-go) — `DNS.Init` + `AddUpdateDomainRecords`,
  YAML config, embedded panel, ~34 providers.

Both dispatch providers with a hard-coded `switch`. OzDDNS adopts their reconciliation and
IP-source ideas but uses a **registry + factory** and one shared **reconciler**. Details:
`docs/03-reference-analysis.md`.

## Development

```bash
make test        # unit tests
make test-race   # with -race
make cover       # coverage report
make vet         # go vet
make fmt         # gofmt
```

## Roadmap

- [x] **Phase 1 — Design**: interactive prototype, design spec, case + test-data design,
      reference analysis.
- [x] **Phase 2 — Backend & CLI**: domain model, provider registry (9 adapters),
      SQLite storage & migrations, reconciler engine + scheduler, REST API, and the
      shared-logic `ozddns` CLI.
- [x] **Phase 3 — Integration & delivery**: panel bound to the live API, `go:embed`,
      Docker/Compose, config export/import, and the test pass over the case catalogue.

## License

TBD.
