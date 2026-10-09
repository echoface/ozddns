# OzDDNS — Design specification

> Status: **design locked for prototype v1**. This document is the source of truth the
> backend and CLI are built against.

Interactive prototype: `web/index.html` (open directly — no build step).

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
generic admin dashboard. Everything is driven by tokens (`web/assets/tokens.css`).

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
├── Sync tasks      list + detail (reconciliation diff, history) + create wizard
├── Executions      rolling-window run log (retention-pruned), filters, run inspector
└── Settings        General · Scheduler · Security · IP sources · Notifications · Storage · About
```

Plus a full-screen **Sign in** view, and system-level **modals / drawers / toasts**.

## 4. Screen inventory

| Screen | Route | Purpose | Primary actions |
|---|---|---|---|
| Overview | `#/overview` | Health at a glance | New task, refresh IP, open failing task |
| Providers | `#/providers` | Manage accounts | Add provider, filter by status |
| Provider detail | `#/providers/:id` (+`/configuration`, `/usage`) | Inspect/validate/edit | Validate, Edit, Delete |
| Sync tasks | `#/jobs` | Manage tasks | Run, toggle, open, New task |
| Task detail | `#/jobs/:id` | See the diff + history | Run now, Edit, Delete |
| New task | `#/jobs/new` | create a sync task | Create task, Cancel |
| Executions | `#/executions` | Audit what happened | Filter, inspect a run |
| Settings | `#/settings/:tab` | Configure instance | Save, test, create token |
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

**Create a task** → *New task* → wizard (5 steps above) → task appears in the list → runs
on the next tick (or *Run now*).

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

REST, JSON, mounted under `/api/v1`. The prototype's mock data already matches these
shapes 1:1.

```
GET    /providers                 -> Provider[]
POST   /providers                 {type,name,config}      -> Provider
GET    /providers/{id}            -> Provider
PATCH  /providers/{id}            -> Provider
DELETE /providers/{id}
POST   /providers/{id}/validate   -> {ok, zones[]} | {error}
GET    /providers/{id}/zones      -> Zone[]
GET    /provider-types            -> ProviderMeta[]        # registry reflection

GET    /ip-sources                -> IPSource[]
POST   /ip-sources  PATCH /ip-sources/{id}  DELETE /ip-sources/{id}

GET    /jobs                      -> Job[]
POST   /jobs                      -> Job
GET    /jobs/{id}                 -> Job
PATCH  /jobs/{id}                 -> Job
DELETE /jobs/{id}
POST   /jobs/{id}/run             -> Run                   # run now
POST   /jobs/{id}/dry-run         -> {diff}                # no writes

GET    /runs?job=&status=&since=  -> Run[]
GET    /runs/{id}                 -> Run (with steps + provider exchange)

GET    /ip                        -> {v4,v6,checkedAt,source}
GET    /status                    -> RuntimeStatus
GET    /settings   PATCH /settings
POST   /settings/test-notify
```

### Domain model (mirrors `web/assets/data.js`)

```ts
type Provider = {
  id, name, type, enabled: boolean,
  status: 'ok'|'warn'|'err', statusText: string,
  zones: string[], records: number,
  config: Record<string, unknown>,   // secrets masked on read
  lastValidated: number, createdAt: number
}
type IPSource = { id, name, type:'http'|'interface'|'command'|'static', family:'v4'|'v6', target, enabled }
type Target  = { domain, type:'A'|'AAAA'|'CNAME', ttl, proxied?, comment? }
type Job = {
  id, name, providerId, ipSourceId, targets: Target[],
  intervalMinutes, runOnChangeOnly, jitter, enabled,
  lastRun, lastStatus:'success'|'nochange'|'failed'|'skipped', nextRun,
  current, desired, error?, lastChanged
}
type Run = { id, jobId, startedAt, durationMs, status, oldIp?, newIp?, trigger:'schedule'|'manual', message?, records? }
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
