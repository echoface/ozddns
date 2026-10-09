# OzDDNS — delivery & operations

This document describes the shipped artifact: how to build it, the CLI and HTTP API
surfaces, the storage and security model, and where each designed case is implemented
and tested.

---

## 1. What ships

A single statically-linked binary, `ozddns`, that embeds:

- the REST API (`/api/v1/*`),
- the management panel (`web/` via `go:embed`),
- the scheduler + reconciler engine,
- the management CLI.

There is one business-logic layer (`internal/app`) and one wiring path
(`app.Open`), so `serve` and the CLI cannot drift apart.

## 2. Build & run

| Task | Command |
|---|---|
| Build | `make build` → `./bin/ozddns` |
| Test | `make test` · `make test-race` · `make cover` |
| Run (demo data) | `./bin/ozddns --db ./ozddns.db serve --demo` |
| Run (clean) | `./bin/ozddns --db ./ozddns.db serve` |
| Container | `docker compose up -d --build` |

Build metadata is injected with `-ldflags -X internal/version.{Version,Build,Commit}`;
`make build` fills them from git/date automatically.

`serve` flags: `--listen`, `--demo`, `--no-scheduler`, `--log-level`.
Global flags: `--db` (or `$OZDDNS_DB`), `--json`.

## 3. CLI reference

```
ozddns serve            [--listen addr] [--demo] [--no-scheduler] [--log-level lvl]
ozddns version
ozddns status                                 # runtime + reconciliation summary
ozddns ip                                     # resolve the host address now
ozddns provider list                          # masked views
ozddns provider add     --type T --name N [--set k=v …] [--disabled]
ozddns provider validate <id>
ozddns provider zones   <id>
ozddns provider rm      <id>
ozddns source list
ozddns source add       --name N --type http|interface|command|static --family v4|v6 --target V
ozddns source test      <id>
ozddns source rm        <id>
ozddns job list
ozddns job show         <id>
ozddns job add          --name N --provider P --source S --target domain[:type[:ttl]] … [--interval m] [--on-change] [--jitter] [--proxied] [--disabled]
ozddns job run          <id> [--dry-run]
ozddns job enable|disable <id>
ozddns job rm           <id>
ozddns run list         [--job id] [--status s] [-n N]
ozddns run show         <id>
ozddns config export    [--out file]          # providers + sources + jobs + settings
ozddns config import    <file> [--replace]
```

`--provider`/`--source` accept an id **or** a name. `--json` prints machine-readable
output on every read command (and on `job run`). A failed reconciliation still records
a run and returns exit code 1, so scripts can detect failures without parsing text.

## 4. HTTP API

All routes are JSON under `/api/v1` and enforce HTTP Basic auth once a password is set.

| Method | Path | Purpose |
|---|---|---|
| GET | `/bootstrap` | everything the panel needs, in one payload |
| GET | `/provider-types` · `/ip-source-types` | registry reflection (drives the add forms) |
| GET | `/ip-sources/interfaces` | local NICs + their addresses, for the wizard's picker |
| GET | `/status` | runtime + run counts |
| GET | `/ip` | current public v4/v6 |
| POST | `/scheduler` | `{running:bool}` pause/resume |
| POST | `/jobs/run-all` | reconcile every enabled task now (sequential; returns an aggregate summary) |
| GET/POST | `/providers` | list / create (validates config) |
| GET/PATCH/DELETE | `/providers/{id}` | read (masked) / update / delete |
| POST | `/providers/{id}/validate` | live credential + zone check |
| GET | `/providers/{id}/zones` | live zone list |
| GET/POST | `/ip-sources` | list / create |
| PATCH/DELETE | `/ip-sources/{id}` | update / delete |
| POST | `/ip-sources/{id}/test` | resolve an address now |
| GET/POST | `/jobs` | list / create |
| GET/PATCH/DELETE | `/jobs/{id}` | read / update / delete |
| POST | `/jobs/{id}/run` | reconcile now (records a manual run) |
| POST | `/jobs/{id}/dry-run` | return the diff, write nothing |
| GET | `/runs?job=&status=&limit=` · `/runs/{id}` | execution history |
| GET/PATCH | `/settings` | read / partial merge update |
| POST | `/settings/password` | set/clear the panel password |
| POST | `/settings/test-notify` | deliver a sample notification |

`GET /bootstrap` also carries a `scheduler` block
(`{running, tickSeconds, workers, runOnStart, defaultIntervalMinutes}`) so the
panel can show the live timer tuning without a second round-trip.

### Scheduler tuning (persisted settings)

The daemon wakes on a ticker and reconciles every task whose interval has
elapsed. The loop is tunable from its own **Settings → Scheduler** tab and stored
in `settings`, so it survives a restart:

| Setting | JSON key | Default | Meaning |
|---|---|---|---|
| Automatic runs | `schedulerPaused` | `false` | master on/off for the timer |
| Check interval | `schedulerTickSeconds` | `5` | how often due tasks are scanned |
| Workers | `schedulerWorkers` | `4` | max concurrent reconciliations |
| Run at start-up | `runOnStart` | `true` | reconcile once when the daemon boots |

`PATCH /settings` applies them live (the running scheduler is reconfigured in
place) *and* persists them; `POST /scheduler` is the quick pause/resume used by
the sidebar switch and the Scheduler tab.

## 5. Storage & security

- **SQLite** via `modernc.org/sqlite` (pure Go — no cgo, so the binary stays static).
  Schema is created by embedded migrations; the repository interfaces keep `*sql.DB`
  out of the business layer so a MySQL backend can be added later.
- **Loopback by default** (`127.0.0.1:9876`). Binding elsewhere logs a warning.
- **Secret masking** happens at the application boundary (`app.MaskConfig`), so the
  API and the CLI both redact credentials; only the request that supplies a secret ever
  sees it. `config export` strips the password hash.
- **Stored secrets are never sent to the browser.** The provider form renders a
  secret that is already on file as an *empty* input plus a `Saved ••••1234`
  chip, so the mask can never be mistaken for the value or "revealed" as one.
  Saving with the field blank (or omitted) keeps the stored credential —
  `app.MergeSecrets` treats an absent, blank or masked secret as "unchanged" —
  while typing a new value replaces it. The reveal (eye) button is hidden until
  the field has content.
- **Passwords** are stored as a salted SHA-256 digest (see `app.SetPassword`).

## 6. Case → implementation → test map

Case IDs are defined in `docs/02-cases-and-testdata.md`.

| Case | Implementation | Test |
|---|---|---|
| PRV-01 add any of 9 types | `internal/providers/all.go`, `provider.Registry.Metas` | `providers.TestRegistryHasAllProviders` |
| PRV-02 validate healthy | `app.validateAndCache`, `cloudflare.Validate` | `cloudflare.TestValidateAndListZones`, `server.TestProviderLifecycle` |
| PRV-03 bad credentials | provider `Validate` error path | `cloudflare.TestValidateRejectsBadToken` |
| PRV-04 expiring token (warn) | `ProviderView.status` | `seed.PrimeDemoHealth` |
| PRV-05 disabled provider | `providerView` → `mute` | `server.TestProviderLifecycle` |
| PRV-06 filter | `web/assets/views.js` (client) | prototype |
| PRV-07 edit config | `app.UpdateProvider`, `save-provider` action | `server.TestProviderLifecycle` |
| PRV-08 delete provider | `app.DeleteProvider` (refuses when in use) | `server.TestProviderLifecycle` |
| PRV-09 used-by tab | `jobView` + `views.js` | prototype |
| PRV-10 secret safety | `app.MaskConfig` | `app.TestMaskConfig`, `server.TestProviderSecretsMasked`, `providers.TestSecretFields` |
| PRV-11 empty zones | `providerView` + `views.js` | prototype |
| SRC-01..06 sources | `internal/ipsource` | `ipsource.TestExtractIP`, `TestStaticSource`, `TestInterfaceSource`, `TestRegistryBuiltins` |
| SRC-10..15 source reuse | `views-settings.js`, `views-wizard.js`, `wiz-create` | prototype + live check (create with a shared source, `Used by` = 1) |
| SRC-14 job delete cascade | `sqlite.jobRepo.Delete` | `sqlite.TestJobDeleteRemovesStateAndRuns` |
| SRC-16..18 source edit + family lock | `app.UpdateIPSource`, `app.IPSourceUsers` | `server.TestIPSourceUpdateAndFamilyLock` |
| SRC-19 responsive tables | `app.css` (`.table-wrap`, `.src-table`), `app.js#compactTables` | prototype (1440 / 1280 / 1100 / 900 / 663 / 412 px) |
| REC-01 no change | `engine.Reconciler.Reconcile` | `engine.TestReconcileNoChange` |
| REC-02 update + diff | " | `engine.TestReconcileUpdate` |
| REC-03 create | " | `engine.TestReconcileCreate` |
| REC-04 partial update | " | `engine.TestReconcilePartialUpdate` |
| REC-05 proxied change | " | `engine.TestReconcileProxiedChange` |
| REC-06 TTL change | `Apply` payload | `cloudflare.TestListRecordsAndApply` |
| REC-07 IPv6 AAAA | `Reconciler` family split | `engine.TestReconcileUpdate` |
| REC-08 wildcard | `provider.LongestZoneMatch` | `provider.TestLongestZoneMatch` |
| REC-09 CNAME | `Reconciler` (content compare) | `engine.TestDryRun` |
| REC-10 skip (no address) | " | `engine.TestReconcileSkipWhenNoAddress` |
| REC-11 provider rejects | " | `engine.TestReconcileFailure` |
| REC-12 dry run | `Reconciler.DryRun` | `engine.TestDryRun`, `cli` dry-run, `server.TestJobFlowRunAndDryRun` |
| SCH-01..05 schedule/run | `engine.Scheduler`, `app.RunJob` | `server.TestJobFlowRunAndDryRun` |
| SCH-06 jitter | `engine.NextRun` | `engine.TestNextRunJitterBounds` |
| SCH-08 interval | " | `engine.TestNextRunInterval` |
| RUN-01..09 executions | `storage.sqlite` runs repo | `sqlite.TestRunsFilterAndCount` |
| RUN-09/09a/09b detail ellipsis + table containment | `app.css` (`.table .cell-detail`, `.table-wrap`, `.exec-table` breakpoints), `views-exec.js` (`title`, flat filter bar) | prototype (1440 / 1200 / 1053 / 900 / 760 / 412 / 375 px — no page-level h-scroll) |
| RUN-10..13 retention | `app.RetentionDays`, `app.PurgeRuns`, `app.RunRetentionLoop`, `RunRepo.DeleteBefore`, `POST /runs/purge`, `views-settings.js` (`s_retention`) | `sqlite.TestRunsDeleteBefore`, `server.TestRunRetention` |
| SRCH-01..09 global search | `web/assets/app.js` (`searchIndex`/`searchRun`/`searchPanelEl`), `app.css` (`.search-panel`) | prototype + live check (tasks / providers / sources / runs / pages, offline mock) |
| SET-01/02 settings | `app.PatchSettings`, `SetPassword` | `server.TestSettingsPatchAndAuth` |
| SET-05 notifications | `app.notify` / `TestNotification` | `server` test-notify |
| UI-01..09 panel states | `web/assets/*` | prototype screenshots in `web/screens/` |
| UI-05a/05b off-canvas drawer dismiss | `app.js` (`.nav-scrim`, `nav-close`), `core.js` (`nav-close` action), `app.css` (`.app.nav-open .nav-scrim`) | prototype (760px: outside click / inside click / `Esc` / nav link; scrim `display:none` at 1440px) |
| WIZ-01..18 new task + edit form | `web/assets/views-wizard.js` + `wiz-create` (POST/PATCH) | manual + `server.TestJobFlowRunAndDryRun` |

## 7. Verification performed

```
make build            # CGO_ENABLED=0, -trimpath, version ldflags applied
make vet              # clean
make test-race        # all packages pass
./bin/ozddns serve --demo
  → GET /api/v1/bootstrap         200, 6 providers · 7 jobs · 9 provider types
  → panel screenshots captured (web/screens/live-*.png)
curl-driven API pass:
  scheduler pause/resume · job PATCH · dry-run · run (skip & fail paths) ·
  provider validate/zones · ip-source create · settings PATCH (merge) ·
  test-notify · password set → 401 → authenticated 200 → cleared
CLI pass:
  provider add · source add · job add · job run --dry-run · job run (failure
  recorded, exit 1) · run list · status · config export → config import
i18n pass:
  en · ja · zh-CN × {overview, providers, jobs, executions, settings,
  settings/security, login} rendered headless with no JS console errors;
  language persists (settings.language) and ?lang= overrides for the session
```

## 7a. Internationalisation

The panel is localised at runtime by `web/assets/i18n.js`. It combines a
`t(phrase, vars)` helper for strings built in JavaScript with a render-time DOM
pass that translates whole static text nodes plus `placeholder` / `title` /
`aria-label` attributes. The catalog is keyed by the English source phrase, so
an untranslated string falls through instead of erroring.

Coverage: navigation and breadcrumbs, every page head and table, buttons and
empty states, the provider catalog (labels, help text, descriptions), the
task/run views, the sign-in screen, all toasts, relative-time helpers
(`just now`, `in 5m`, …) and date formatting (`zh-CN` / `ja-JP` locales).

Switching: **Settings → General → Language** writes `settings.language` (codes
`en` / `zh-CN` / `ja`; the old `English` / `简体中文` / `日本語` values are still
accepted and normalised). `?lang=<code>` pins the language for a session.
Precedence: `?lang=` → localStorage → server setting → `navigator.language`.

Screen-specific copy that lives in the server (e.g. a provider's raw
`statusText` from an upstream API error) is intentionally left verbatim.

Add a language: append a code + label near the top of `i18n.js` and a catalog
object; no other file changes are required.

## 7b. Theme (light / dark / auto)

The visual language ships with two palettes and an automatic mode, wired by
`web/assets/theme.js`:

- **`data-theme` on `<html>`** flips the design tokens in
  `web/assets/tokens.css` (`--paper`, `--ink`, `--card`, `--coral`, semantic
  colours, shadows). The dark palette is a warm charcoal — not a generic
  black — so the editorial/terracotta identity survives.
- **Toggle**: the moon/sun button in the topbar flips light↔dark and writes the
  choice back to `settings.theme` (persisted server-side, mirrored to
  `localStorage`). **Settings → General → Theme** offers `Light` / `Dark` /
  `Auto`.
- **Auto** follows the OS via `prefers-color-scheme` and re-paints live when the
  system switches. `?theme=<code>` pins a theme for a session (handy for
  screenshots), mirroring the `?lang=` override.
- Precedence: `?theme=` → localStorage → server setting → `light`.

## 7c. First paint (no mock/default flash)

The SPA is gated on its single `/bootstrap` fetch. `OZ.state.boot` starts at
`loading` and the shell renders a **skeleton** (same sidebar / topbar / content
geometry) instead of the bundled mock dataset, so the panel never flashes
placeholder rows, zero counts or default form values before the real data
arrives. When `bootstrap` resolves, `applyBootstrap` sets `boot = 'ready'` and
the first real paint happens.

- Server **unreachable** → `boot = 'offline'`: the bundled mock dataset renders,
  so the prototype still opens straight from the filesystem (`web/index.html`).
- Server reachable but **401** → `boot = 'ready'` and the router lands on
  `#/login`; the sign-in action stays on the live path (it is gated on
  `boot === 'offline'`, not on `/bootstrap` having succeeded).
- A `?theme=` / `?lang=` override is still applied before the first paint, so
  the skeleton is already in the right language and palette.

## 8. Limits & follow-ups

- `MySQL` storage is designed for (the interface allows it) but not implemented.
- The panel's "API token" issuing is a UI stub (the Executions export/saved-view
  buttons were removed — see RUN-07).
- Notification delivery is a generic JSON POST; Shoutrrr-style URL parsing is future work.

## 9. Troubleshooting

### Cloudflare: `status 400 … "Invalid format for Authorization header"` (codes 6003 / 6111)

Cloudflare has **two different credential shapes** and they use different headers:

| Credential | Looks like | Sent as |
|---|---|---|
| **API Token** (recommended) | ~40 chars, base64-ish | `Authorization: Bearer <token>` |
| **Global API Key** (legacy) | exactly **37 hex chars** | `X-Auth-Key: <key>` **+** `X-Auth-Email: <email>` |

Pasting a Global API Key into the **API Token** field produces
`Authorization: Bearer <37-hex-key>`, which Cloudflare rejects with the message above.
OzDDNS now detects the 37-hex shape at add/validate time and refuses the config with an
explicit instruction instead of forwarding the confusing 400.

Correct either way:

- **Token**: `provider add --type cloudflare --name CF --set api_token=<40-char token>`
  (create it under *My Profile → API Tokens* with `Zone.DNS Edit`; leave the email blank).
- **Global API Key**: `--set email=you@example.com --set api_key=<37-hex key>`
  (*My Profile → API Tokens → Global API Key*).

### "Validated" but nothing updates

A provider can be reachable while a *task* still fails, and vice versa. Check
`ozddns run list` — a `failed` row carries the provider's own error text. Common causes:
the token lacks `Zone.DNS Edit` for that specific zone, the zone is not in the account,
or the record name is outside any zone the credential can see (see `ozddns provider zones <id>`).

### Editing a provider broke its credentials

Fixed: secret fields shown as `••••1234` are no longer written back. When the panel saves a
form whose secret is still masked, the stored value is preserved (`app.MergeSecrets`).
