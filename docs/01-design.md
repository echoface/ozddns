# OzDDNS — Design specification

> Status: **design locked**. This document is the source of truth the
> backend and CLI are built against.

Panel implementation: `web/v2/` (Vue 3 + TypeScript, built with Vite).

---

## 1. Product definition

OzDDNS is a **provider-centric, single-binary dynamic DNS manager**.

The mental model is *reconciliation*, not *polling*: for every task, the engine reads the
DNS record that actually exists, reads the address the host actually holds, and writes a
record **only if they disagree**. The UI is built around showing that diff honestly.

Three first-class nouns (never conflated):

| Noun | Meaning | Example |
|---|---|---|
| **Provider** | A DNS account/credential, validated once | *Homelab Cloudflare* |
| **IP source** | Where an address comes from | *ipify (IPv4)*, *LAN interface · en0* |
| **Sync task** | provider + source + records + schedule | *Home IPv4* |

Runs (executions) are the append-only record of what the engine did.

## 2. Design language — "warm editorial"

The palette and type are chosen to feel like a considered print product rather than a
generic admin dashboard. Everything is driven by tokens
(`web/v2/src/compat/tokens.css`).

### Color

| Token | Value | Use |
|---|---|---|
| `--paper` | `#F3F0E8` | app canvas (warm ivory), with soft radial washes |
| `--card` | `#FFFFFF` | raised cards |
| `--ink` | `#1B1A16` | primary text |
| `--ink-2` | `#56534B` | secondary text |
| `--coral` | `#D97757` | primary accent (actions, active nav) |
| `--olive` | `#5F7C4E` | success / updated |
| `--amber` | `#B8862B` | attention / expiring |
| `--rust` | `#B14630` | failure / destructive |
| `--slate` / `--violet` | `#4F6B7E` / `#6E5A86` | A / AAAA protocol labels, info |

Rules: accent color is used sparingly (primary buttons, active nav, one eyebrow label).
Status is always color **plus** a dot/label — never color alone.

### Type

- Display / headings: **Fraunces** (serif), tight tracking, weights 400–600.
- Body / UI: **Instrument Sans**, 13–15px.
- Mono: system UI mono, for IPs, zone names, tokens, diffs.

Both display and body fall back to OS serif/sans so the panel renders acceptably offline.

### Shape & motion

- Radii: 9 / 12 / 16 / 22px; pills for status.
- Borders are hairline (`--line`) with very soft shadows; the layout relies on space, not
  boxes-on-boxes.
- Motion: one entrance reveal per view (`rise`, staggered), toast slide-in, modal pop.
  All respects `prefers-reduced-motion`.

## 3. Information architecture

```
OzDDNS
├── Overview        control room: live IP, health, task list, activity
├── Providers       provider instances (accounts) + detail (config / zones / usage)
├── Sync tasks      list + detail (reconciliation diff, history) + create/edit form
├── Executions      rolling-window run log (retention-pruned), filters, run inspector
└── Settings        General · Scheduler · Security · IP sources · Notifications · Storage · About
```

Plus a full-screen **Sign in** view, and system-level **modals / drawers / toasts**.

### Access modes and authorization

The daemon has one deliberate default: bind to `127.0.0.1:9876` and run in open
mode. A local-only deployment therefore needs no account, login screen, token,
or configuration before it is useful.

Here, "daemon" means the single long-running `ozddns serve` process. It owns the
HTTP server, scheduler, reconciler, and execution-history retention loop. There
is no separate OzDDNS supervisor process; systemd, Docker, or another service
manager may supervise it.

Authorization is an explicit opt-in in **Settings → Security**. Enabling it
creates a single administrator through a password → TOTP enrollment flow, then
returns ten one-time recovery codes exactly once. Sign-in uses the password to
obtain a five-minute challenge, verifies TOTP or a recovery code, and finally
sets an opaque server-side session cookie. The cookie is `HttpOnly` and
`SameSite=Lax`; the database stores only its SHA-256 hash.

Sessions slide on a 30-minute idle timeout but are capped at 12 hours absolute.
TOTP time steps and recovery codes are single-use. Turning authorization off
requires both factors and atomically deletes the administrator, sessions,
pending challenge, TOTP secret, and recovery codes. Re-enabling always performs
a fresh enrollment with a different TOTP secret. Local filesystem access to the
database is the break-glass path: `ozddns auth disable --yes`.

Authorization protects the HTTP management surface only. The scheduler,
reconciler, and notification delivery continue after the browser signs out.
There is one administrator and no role or standalone API-token model; scripts
calling protected endpoints must complete the login handshake and retain the
session cookie.

The HTTP adapter also applies a hard request-body limit (64 KiB for auth
endpoints and 8 MiB for the rest of `/api/v1`), `no-store` on authorized API
responses, and browser hardening headers that deny framing and plugin content.
Oversized requests fail with `413` before JSON decoding, including chunked
requests without a `Content-Length`.

Enrollment endpoints are loopback-only. This closes the otherwise obvious
takeover path where an operator exposes an open instance before creating the
administrator and a remote client enrolls first.

If an open instance is bound beyond loopback, the CLI warns at startup and the
panel shows a global banner. A bind that is reachable only on networks the
operator controls — a LAN, or an overlay such as Tailscale — can declare
`trustedNetwork` (`--trusted-network` / `OZDDNS_TRUSTED_NETWORK`) to suppress
that warning; it is a trust assertion, not access control. Session cookies
become `Secure` when the request
is TLS or when `--trust-proxy` trusts an HTTPS forwarded scheme. The same flag
also makes client classification use `X-Forwarded-For` for rate limiting and
loopback-only enrollment. Enable it only when every request arrives through a
trusted proxy that overwrites both forwarding headers; trusting them on a
directly reachable listener permits source-address spoofing.

## 4. Screen inventory

| Screen | Route | Purpose | Primary actions |
|---|---|---|---|
| Overview | `#/overview` | Health at a glance | New task, refresh IP, open failing task |
| Providers | `#/providers` | Manage accounts | Add provider, filter by status |
| Provider detail | `#/providers/:id` (+`/configuration`, `/usage`) | Inspect/validate/edit | Validate, Edit, Delete |
| Sync tasks | `#/jobs` | Manage tasks | Run, toggle, open, New task |
| Task detail | `#/jobs/:id` | See the diff + history | Preview, Run now, Edit, Delete |
| New task | `#/jobs/new` | create a sync task | Create task, Cancel |
| Executions | `#/executions` | Audit what happened | Filter, inspect a run |
| Settings | `#/settings/:tab` | Configure instance | Save, test, backup/restore |
| Sign in | `#/login` | Authenticate | Sign in |

### Global search

The top bar carries one search box (a `/`-focusable jump-to, not a query
language). It indexes what the panel has already loaded — sync tasks,
providers, IP sources, the newest executions and every nav page — and lists
the best 8 matches with a kind chip (`Task` / `Provider` / `IP source` /
`Execution` / `Page`). `↑`/`↓` move the highlight, `Enter` opens it, `Esc`
dismisses. Because the index is built from the live datasets, it also works
against the offline mock. Pages that used to carry their own lookalike search
(Providers) now defer to it, so there is exactly one box.

### Execution history & retention

Executions is a **rolling window**, not an archive: the daemon keeps
`runRetentionDays` days (General setting, default **1**, clamp 1–365) and then
deletes the rest for real — `DELETE FROM sync_runs WHERE started_at < cutoff`,
run 30 s after boot and hourly thereafter. The page makes the window explicit
rather than offering an export it doesn't have: the sub-title reads *"History
is kept for … and purged hourly"*, and the **Total runs** stat is scoped to it.
The table is contained by a `.table-wrap`, so it can never paint outside the
card: the Detail column (capped on an inner block — `max-width` is ignored on a
table cell) is ellipsised with the full text on `title`, the filter bar is a
single wrapping `.card-head`, and the low-value columns drop out as the card
narrows (Trigger ≤1200px, Duration ≤1100px). The page never scrolls sideways.

### New task form

One screen, four decisions — the earlier 5-step wizard was out of proportion to
what a dynamic-DNS task actually needs (ddns-go is the reference here):

1. **IP version** — IPv4 · A or IPv6 · AAAA. The record type is *derived* from the
   family, never chosen, so a task can't contradict itself. It is locked whenever a
   stored source is reused (the source's family wins).
2. **DNS provider** — dropdown of enabled providers; repeated names carry an id tail.
3. **Domains** — one per line; each becomes a record of the derived type.
4. **IP source** — a single dropdown: *Create a new source* (kind + target controls
   appear inline) or any stored source, which is then **shared** rather than
   duplicated. The reused card shows family, target, a **Test** button and how many
   tasks already use it.

The server enforces the same invariant for every client: a task's targets must all
match its IP source family. The UI already derives the record type, but API, CLI
and configuration-import callers are validated too, so malformed input cannot
write an IPv4 value into an `AAAA` record or the reverse.

The same form doubles as the editor: `#/jobs/{id}/edit` opens it pre-filled from
the stored task (button: **Save changes**), so a task never has two places to be
changed from. Stored sources are editable too, and because a source can feed
several tasks its address family is locked while any of them reference it — the
change that *is* allowed (name, kind, target) lands on those tasks' next run,
which the sheet says out loud.

Everything else (interval, write mode, run-on-start, jitter) hides behind
**Advanced options**, so the common case stays four fields.

## 5. Key flows

**Add a provider** → *Add provider* → choose type (grid of compiled-in providers) →
enter credentials → *Add & validate* → provider validated, appears as a card.

**Create a task** → *New task* → one-page form (the four decisions above) →
task appears in the list → runs on the next tick (or *Run now*).

**Resolve a failure** → Overview banner *"1 task needs attention"* → open task → see
`last run failed` banner → open the execution → read the provider exchange (e.g. `401`) →
Edit provider credentials → Validate → Re-run.

**Inspect a change** → Executions → click a run → drawer shows old→new, the four engine
steps, and the provider request/response.

## 6. States that must be designed (all implemented in the prototype)

- First run / empty: no providers, no tasks.
- Loading: skeletons for cards and tables.
- Success / no-change / skipped / failed — four distinct run states, with distinct copy.
- Partial: a task writing 2 records where only 1 drifted.
- Degraded provider: warn (expiring token) and error (invalid credentials) on the card and
  detail, driving the Overview banner.
- Disabled: paused tasks and providers are visibly muted, never hidden.
- Long content: many runs, long IPv6 strings (wrap), many provider types (grid).
- Destructive: delete provider/task behind a confirm modal that explains the consequence.

## 7. Backend contract (what the UI expects)

REST, JSON, mounted under `/api/v1`. `GET /healthz` is a separate public
liveness probe outside that prefix. The public authentication surface is
`/auth/state`, the setup handshake, login, MFA, and logout; every other API
route requires a session when authorization is enabled.

```
GET    /healthz                   -> liveness (public, outside /api/v1)

GET    /auth/state                -> AuthState              # public mode state
POST   /auth/setup/start          -> TOTP enrollment material
POST   /auth/setup/verify         -> {recoveryCodes,user}
POST   /auth/setup/cancel
POST   /auth/login                -> {mfaRequired,challenge}
POST   /auth/mfa                  -> {ok,user} + session cookie
POST   /auth/logout
GET    /auth/me                   -> {username}
POST   /auth/disable
POST   /auth/recovery-codes
GET    /auth/events               -> AuthEvent[]

GET    /providers                 -> Provider[]
POST   /providers                 {type,name,config}      -> Provider
GET    /providers/{id}            -> Provider
PATCH  /providers/{id}            -> Provider
DELETE /providers/{id}
POST   /providers/{id}/validate   -> {ok, zones[]} | {error}
GET    /providers/{id}/zones      -> Zone[]
GET    /provider-types            -> ProviderMeta[]        # registry reflection

GET    /ip-sources                -> IPSource[]
POST   /ip-sources                 -> IPSource
PATCH  /ip-sources/{id}            -> IPSource
DELETE /ip-sources/{id}
POST   /ip-sources/{id}/test       -> {ok,address,error?}
GET    /ip-sources/interfaces      -> Interface[]
GET    /ip-source-types            -> SourceMeta[]

GET    /jobs                      -> Job[]
POST   /jobs                      -> Job
GET    /jobs/{id}                 -> Job
PATCH  /jobs/{id}                 -> Job
DELETE /jobs/{id}
POST   /jobs/{id}/run             -> Run                   # run now
POST   /jobs/{id}/dry-run         -> {targets[]}           # no writes
POST   /jobs/run-all              -> {results,failed}

GET    /runs?job=&status=&limit=  -> Run[]
GET    /runs/{id}                 -> Run
POST   /runs/purge                -> {ok,deleted,keptDays}

GET    /ip?v4SourceId=&v6SourceId= -> {v4:{address,source,status},v6:{...},checkedAt}
GET    /status                    -> RuntimeStatus
GET    /settings   PATCH /settings
POST   /settings/test-notify
POST   /scheduler                 -> {running}
GET    /config/export
POST   /config/import
```

### Design decisions and deliberate boundaries

| Decision | Rationale and consequence |
|---|---|
| Server-side opaque sessions instead of JWT | Authorization can be revoked immediately by deleting one session or all sessions; no bearer credential or signing key is exposed to browser JavaScript. The cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` on HTTPS. |
| Open mode by default, loopback bind by default | A local single-user install is useful without security setup to complete. Open mode is not a supported exposure model; the CLI warns and the panel banners an open listener that is reachable beyond loopback, unless the operator asserts a `trustedNetwork` bind reachable only on networks they control. |
| Authorization is opt-in and protects HTTP only | DNS scheduling is the product's core job and must not stop merely because an operator is signed out. Authorization gates management mutations and reads, not the background engine. |
| First enrollment is loopback-only | An exposed but unenrolled instance must not be claimable by the first remote visitor. A trusted reverse proxy can preserve the real client address only when `trustProxy` is enabled and the proxy overwrites forwarded headers. |
| One administrator, no roles or API tokens | This keeps the current security model explicit and auditable. Multi-user tenancy, delegated permissions, and machine tokens require a separate design rather than an ad-hoc extension. |
| Disable deletes all authorization state | No dormant password, TOTP secret, recovery code, challenge, or session survives a transition back to open mode. Re-enabling is a new enrollment. |
| Overview IP reflects the selected source per family | The operator can inspect any configured IPv4 or IPv6 source, whether or not a sync task uses it. The browser stores the two source choices in `localStorage` because this is presentation state, not instance configuration. Without a stored choice, the first configured source for each family is shown; if none exists, the endpoint returns `no_source`. |
| Replace import is serialized with runs and edits | The transaction makes the database replacement atomic, while an application-level read/write lock prevents an in-flight run from persisting history for a task the import just deleted. Imported scheduler settings are applied before the import releases that lock. |
| SQLite and in-process coordination | The deployment target is a single process. The login limiter is an in-memory, per-source-IP token bucket with a hard 8192-entry cap and expired/old-entry eviction. A bitmap cannot represent per-IP counters or time windows; a multi-instance deployment would need a shared limiter and a different session architecture. |
| Bounded HTTP and audit inputs | Auth payloads are capped at 64 KiB and other API payloads at 8 MiB so unauthenticated JSON parsing cannot grow without bound. Security audit rows retain the newest 10,000 entries; failed sign-ins are still recorded, but rotating source addresses cannot grow the database indefinitely. |
| TLS terminates at a trusted proxy | OzDDNS does not carry certificates. `trustProxy` is a narrow trust decision, not a generic way to make an HTTP listener public. |
| IP sources have no enabled flag | A source is a reusable read strategy, while the task's `enabled` flag decides whether automatic reconciliation runs. Keeping both controls made the panel hide a source while existing tasks still used it, so the redundant state was removed instead of adding a second execution rule. |

### Domain model (mirrors `web/v2/src/types.ts`)

```ts
type Provider = {
  id, name, type, enabled: boolean,
  status: 'ok'|'warn'|'err'|'mute', statusText: string,
  zones: string[], records: number,
  config: Record<string, unknown>,   // secrets masked on read
  lastValidated: number, createdAt: number
}
type IPSource = { id, name, type:'http'|'interface'|'command'|'static', family:'v4'|'v6', target }
type Target  = { domain, type:'A'|'AAAA', ttl, proxied?, comment? }
type Job = {
  id, name, providerId, ipSourceId, targets: Target[],
  intervalMinutes, runOnChangeOnly, jitter, enabled,
  lastRun, lastStatus:'success'|'nochange'|'failed'|'skipped'|'', nextRun,
  current, desired, error?, lastChanged
}
type Run = {
  id, jobId, startedAt, durationMs,
  status:'success'|'nochange'|'failed'|'skipped', oldIp?, newIp?,
  trigger:'schedule'|'manual', message?, records?,
  details: { domain, type, action:'created'|'updated'|'unchanged'|'skipped'|'failed',
             current?, desired?, proxied?, note? }[]
}
```

## 8. Accessibility & responsiveness

- Targets WCAG AA contrast for body text; status never relies on hue alone.
- Full keyboard path: nav is anchors, actions are buttons, `/` focuses search, `Esc`
  closes modals (or the open drawer); visible focus ring (`--ring`) on inputs and controls.
- Responsive: 3-col grids collapse to 2 then 1; the sidebar becomes an off-canvas drawer
  below 900px; the login split collapses to a single column.
- The off-canvas drawer is dismissible the way a drawer should be: a `.nav-scrim`
  covers everything below it, so tapping the content, the top bar or a table closes
  it — and so does `Esc`, or following any nav link (a route change resets `navOpen`).
  The scrim only exists under the 900px breakpoint, where the drawer is off-canvas.
