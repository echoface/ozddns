# OzDDNS — Case catalogue & test-data design

Two goals:

1. Every behaviour the UI shows has a named, designed **case** (so nothing is "implied").
2. Every case is backed by concrete **seed data** that the prototype already renders, and
   that the Go test suite will reuse as fixtures.

The seed lives in `web/assets/data.js`. Case IDs are stable and used to tag both
prototype fixtures and (later) Go tests: `TestPRV_02_...`, e2e `spec(PRV-02)`.

---

## 0. Seed data (the canonical fixture set)

### Provider instances

| id | name | type | status | enabled | zones | records | note |
|---|---|---|---|---|---|---|---|
| `p-cf` | Homelab Cloudflare | cloudflare | ok | ✔ | example.com, homelab.net | 6 | proxied on |
| `p-ali` | Aliyun DNS · 主站 | alidns | ok | ✔ | ozhao.cn | 9 | 2 jobs |
| `p-dnspod` | Tencent DNSPod | dnspod | **warn** | ✔ | family.site | 3 | token expires in 6 days |
| `p-gd` | GoDaddy (legacy) | godaddy | **err** | ✘ | — | 0 | invalid creds (401) |
| `p-duck` | DuckDNS | duckdns | ok | ✔ | ozddns | 1 | |
| `p-hook` | NAS Callback | custom | ok | ✔ | — | 0 | webhook |

### IP sources

| id | name | type | family | target |
|---|---|---|---|---|
| `src-ipify-v4` | ipify (IPv4) | http | v4 | `https://api.ipify.org` |
| `src-ifconfig-v6` | ifconfig.co (IPv6) | http | v6 | `https://v6.ifconfig.co` |
| `src-en0` | LAN interface · en0 | interface | v4 | `en0` |
| `src-cmd-v6` | Tailscale address | command | v6 | `tailscale ip -6` |

### Tasks & their intended outcome

| id | name | provider | source | records | interval | lastStatus | teaches |
|---|---|---|---|---|---|---|---|
| `j-home-v4` | Home IPv4 | p-cf | src-ipify-v4 | home A | 5m | nochange | steady state |
| `j-home-v6` | Home IPv6 | p-cf | src-ifconfig-v6 | home AAAA | 5m | nochange | AAAA + long string wrap |
| `j-nas` | NAS & CDN edge | p-ali | src-en0 | nas, cdn A | 10m | success | **partial** update (2 records) |
| `j-blog` | Blog (DNSPod) | p-dnspod | src-ipify-v4 | blog A | 15m | **failed** | auth error surface |
| `j-wild` | Wildcard .ozhao.cn | p-ali | src-ipify-v4 | `*.ozhao.cn` A | 30m | nochange | **disabled** task |
| `j-iot` | IoT callback | p-hook | src-en0 | iot CNAME | 60m | success | non-A record, webhook |
| `j-tail` | Tailscale node | p-duck | src-cmd-v6 | ozddns AAAA | 2m | **skipped** | no address available |

### Runs

22 runs (`r-80`…`r-101`) spanning ~5h, deliberately including: a real IP change
(`203.0.113.9 → 203.0.113.42`), repeated auth failures, a webhook timeout, skipped runs,
no-op runs, and one manual run created at runtime by the *Run now* action.

---

## 1. Provider management

| ID | Case | Expected |
|---|---|---|
| PRV-01 | Add a provider of each supported type | wizard lists all 9 types; type-specific fields shown |
| PRV-02 | Validate a healthy provider | pill → *Connected*, `statusText` = "Validated just now" |
| PRV-03 | Validate with bad credentials (`godaddy`) | toast **error**, pill stays *Error* |
| PRV-04 | Provider with expiring token | `warn` pill + detail banner "rotate before it expires" |
| PRV-05 | Disabled provider | muted *Disabled* pill; excluded from the wizard's picker |
| PRV-06 | Filter providers: All / Connected / Attention / Errors | list and counts update per filter |
| PRV-07 | Edit configuration | stored secrets shown as an empty input + `Saved ••••1234` chip; blank save keeps the credential; new value replaces it |
| PRV-08 | Delete provider | confirm modal warns "jobs that reference it must be reassigned" |
| PRV-09 | Provider used by N tasks | *Used by (N)* tab lists them, click navigates to task |
| PRV-10 | Secret safety | secrets never shown in plaintext by default; API responses redact `config` |
| PRV-11 | Empty zones | detail shows "No zones discovered", records = 0 |

## 2. IP sources

| ID | Case | Expected |
|---|---|---|
| SRC-01 | HTTP lookup source | shows URL, family chip |
| SRC-02 | Network-interface source | wizard lists the host's real NICs + addresses in a picker (ddns-go style) |
| SRC-03 | Shell-command source | shows command; first match extracted |
| SRC-04 | IPv6 source | violet `V6` chip; address wraps in the UI |
| SRC-05 | "Used by N" column | counts jobs referencing the source |
| SRC-06 | Copy target | copy button copies the target string, toast confirms |
| SRC-07 | Three strikes | after 3 consecutive read failures the task is marked failed **once** |
| SRC-08 | Interface with several addresses | each family's addresses are listed separately; `en0#1` selects the 2nd (`parseIfaceTarget`) |
| SRC-09 | No usable NIC addresses | picker falls back to a plain NIC-name input with an explanatory help line |
| SRC-10 | Sources tab explains itself | an intro banner states what a source is and that sources are reusable across tasks |
| SRC-11 | Row **Test** | resolves the address now (`POST /ip-sources/{id}/test`) and toasts the value, without touching the provider |
| SRC-12 | Row **Delete** | confirm modal warns referencing tasks must be reassigned; the list refreshes |
| SRC-13 | No sources yet | empty state with an "Add source" call to action |
| SRC-14 | Deleting a job cascades | its state row and run history go too, so the activity feed never shows a task that no longer exists |
| SRC-15 | Sources are actually shared | the task form can point a new task at a stored source, and **Used by** then counts it |
| SRC-16 | Row **Edit** | opens the sheet pre-filled; renaming or re-pointing a source saves in place, and the warning banner says how many tasks will pick the change up on their next run |
| SRC-17 | Address family is locked | flipping v4 ⇄ v6 is refused while tasks reference the source (it would contradict their A / AAAA records); removing the last reference unlocks it |
| SRC-18 | Type dropdown by name | the sheet lists source kinds by label ("HTTP lookup", "Network interface", …) rather than raw ids |
| SRC-19 | Sources table at any width | headers stay on one line; the type column gives way first, then the target moves under the name, so the row never wraps into a ragged stack — the card scrolls only on phones |

## 3. New task (single page, ddns-go style)

The form is one screen with four essential fields — the heavy 5-step wizard was
replaced because it was out of proportion to the actual decision:

| ID | Case |
|---|---|
| WIZ-01 | **IP version** segmented control (IPv4 · A / IPv6 · AAAA) — sets the record type for every domain |
| WIZ-02 | **DNS provider** is a dropdown of enabled providers; duplicate names are disambiguated by id tail |
| WIZ-03 | **Domains** is a textarea, one domain per line (also splits on `,` and `;`); the help line counts the records |
| WIZ-04 | **IP source** = a kind dropdown + a value control that swaps with the kind |
| WIZ-05 | *Network interface* shows a picker of detected addresses for the chosen family |
| WIZ-06 | The record type is never a free choice: the job's targets inherit A / AAAA from the IP version |
| WIZ-07 | Provider/domain validation toasts before any request is sent ("Pick a DNS provider", "Add at least one domain") |
| WIZ-08 | Task name is optional and defaults to the first domain |
| WIZ-09 | **Advanced options** (collapsed) holds interval, write mode, jitter and run-on-start |
| WIZ-10 | No providers → a "No providers yet" banner with a shortcut to Providers |
| WIZ-11 | `Create task` creates the IP source then the job, toasts and routes to the task list; Cancel returns to the list |
| WIZ-12 | Blank HTTP URL / shell command fall back to sane defaults (`https://api.ipify.org`, `tailscale ip -6`) |
| WIZ-13 | **IP source** is a single dropdown: "Create a new source" (the default) plus every stored source, labelled `name · family · target` |
| WIZ-14 | Picking a stored source reuses it — no duplicate row is created, and the task resolves it on every run |
| WIZ-15 | Reusing a source locks **IP version** to that source's family (the two can never contradict) and shows the source's family chip, target and a **Test** button |
| WIZ-16 | The reused-source box notes how many tasks already use it, so sharing is visible before you commit |
| WIZ-17 | Editing a task (`#/jobs/{id}/edit`) opens the same form pre-filled from the stored task — name, provider, reused source, family, domains and interval; the button reads **Save changes** and `Cancel` returns to the task |
| WIZ-18 | Field order matches the decision: task name → IP version / provider → **IP source** → domains → advanced |

### Task detail page

| ID | Case |
|---|---|
| DTL-01 | Each action has exactly one entry point: **Run now / Edit / Delete** live only in the page header |
| DTL-02 | The coordination card is read-only (records table + last/next line) — no duplicated "reconcile now" button |
| DTL-03 | The configuration card is a read-only spec sheet — no duplicated "edit task" button |
| DTL-04 | Configuration is stacked above the coordination table on narrow screens (info → status) |

## 4. Reconciliation (core)

| ID | Case | Seed |
|---|---|---|
| REC-01 | Desired == current → **no write** | `j-home-v4` |
| REC-02 | Desired != current → **update**, show diff | run `r-94` |
| REC-03 | Create a record that does not exist yet | provider returns empty list |
| REC-04 | Two records, only one drifted → partial update | `j-nas` |
| REC-05 | Proxied flag changed but IP same → still a write | `r-98` |
| REC-06 | TTL change only | edit a target's TTL |
| REC-07 | IPv6 record, long address | `j-home-v6` |
| REC-08 | Wildcard name `*` | `j-wild` |
| REC-09 | CNAME target (non-address) | `j-iot` |
| REC-10 | No address available → **skip** (not failure) | `j-tail` |
| REC-11 | Provider rejects write → **failed** | `j-blog` |
| REC-12 | Dry-run returns diff without writing | `POST /jobs/:id/dry-run` |

## 5. Scheduling & runtime

| ID | Case |
|---|---|
| SCH-01 | Scheduler running: next-run countdowns shown per task |
| SCH-02 | Pause scheduler: sidebar flips state, next-run shows "paused", no auto-runs |
| SCH-03 | Disable one task: `nextRun` cleared, row muted, excluded from Run all |
| SCH-04 | "Run all" (Sync tasks page only): reconciles every enabled task, summarises counts |
| SCH-05 | Run one task: creates a `manual` run at the top of history |
| SCH-06 | Jitter on/off reflected in configuration |
| SCH-07 | Run-on-start reflected in configuration |
| SCH-08 | Interval change reflected in list + wizard |

## 6. Executions & logging

| ID | Case |
|---|---|
| RUN-01 | Four statuses render distinctly: Updated / No change / Skipped / Failed |
| RUN-02 | Filter by status (All · Success · No change · Failed · Skipped) |
| RUN-03 | Filter by task |
| RUN-04 | Open a run: drawer shows old→new, duration, 4 steps, provider exchange |
| RUN-05 | Failed run shows the provider error (401 / timeout) in red |
| RUN-06 | Re-run from the drawer |
| RUN-07 | No dead affordances: the header carries no Export CSV / Saved views buttons — the log is a rolling window, not an export surface |
| RUN-08 | Runs are sorted newest-first |
| RUN-09 | Detail column never sprawls: the cap lives on an inner block (`max-width` is ignored on a `<td>`), it is ellipsised, and the full text is on the cell's `title` so hover reveals it |
| RUN-09a | The runs table **fits its card** at every width: it lives in a `.table-wrap` so it can never paint past the card background, and the low-value columns give way first (Trigger ≤1200px, Duration ≤1100px, tightening the detail cap). Only phones scroll, and the page itself never scrolls sideways |
| RUN-09b | The filter bar is one flat `.card-head`, never a card nested in a card, and it wraps (`flex-wrap`) instead of pushing the row wider than the parent |
| RUN-10 | Retention window is a setting (`runRetentionDays`, default **1**, 1–365). Out-of-range values are rejected with a 400 |
| RUN-11 | The daemon prunes execution history **for real** — `DELETE FROM sync_runs WHERE started_at < cutoff` — 30s after boot and then **hourly**, so the DB cannot grow without bound (`app.PurgeRuns`, `app.RunRetentionLoop`) |
| RUN-12 | `POST /api/v1/runs/purge` runs the same purge on demand and returns `{ok, deleted, keptDays}` |
| RUN-13 | The page states the window: the sub-title reads "History is kept for … and purged hourly", and the **Total runs** stat is scoped to that window rather than a hardcoded 24h |

## 7. Settings & security

| ID | Case |
|---|---|
| SET-01 | General: language, timezone, default interval, listen address |
| SET-01b | General: **Execution history (days)** — a bounded number field (1–365) whose value the scheduler hands to the hourly retention purge |
| SET-01a | Scheduler tab: pause/resume, tick seconds, workers, run-at-start-up (persisted) + "How it works" notes |
| SET-02 | Security: username, change password, require-login, TLS |
| SET-03 | Loopback default warning + "Open to LAN/WAN" toggle |
| SET-04 | Create API token: shown once, "copy now" warning |
| SET-05 | Notifications: channel list with trigger chips; Test delivers a sample |
| SET-06 | Storage: driver/path; export / import / snapshot; danger-zone reset |
| SET-07 | About: version, build, runtime, installed providers |
| SET-08 | Theme: light / dark / auto; topbar moon-sun toggle persists to `settings.theme` |

## 8. System / UI states

| ID | Case |
|---|---|
| UI-01 | First-run empty (no providers, no tasks) |
| UI-02 | Boot skeleton shown until `/bootstrap` resolves — the mock dataset (and zero-value defaults) never flash on first paint |
| UI-03 | Toasts for success / info / error |
| UI-04 | Confirm modal for destructive actions |
| UI-05 | Off-canvas nav under 900px; grids collapse |
| UI-05a | Tapping anywhere outside the open drawer dismisses it — a `.nav-scrim` covers the viewport below the drawer, so a click on the content, top bar or table closes it |
| UI-05b | The drawer also closes on `Esc`, on any nav link (route change), and the scrim never exists at ≥900px |
| UI-06 | Keyboard: `/` focuses search, `Esc` closes modal (or the drawer, if that is what is open) |
| UI-07 | `prefers-reduced-motion` disables entrance animation |
| UI-07a | Dark theme: every route renders with the charcoal palette (no light flashes) |
| UI-08 | Unknown route → "Page not found" |
| UI-09 | Unknown provider/task id → not-found state with recovery link |
| UI-10 | Providers page has **one** search box (the top-bar jump-to); the in-page box is gone, replaced by a hint that says so |

## 8a. Global search (top bar)

One box, indexed from whatever the panel has loaded. It is a *jump-to*, not a
query language: the query is matched case-insensitively against a hit's label,
sub-title and kind, and the best 8 are listed.

| ID | Case |
|---|---|
| SRCH-01 | **Tasks** — one hit per sync task (label = name, sub = its domains / provider) → `#/jobs/{id}` |
| SRCH-02 | **Providers** — one hit per provider (sub = type · zones) → `#/providers/{id}` |
| SRCH-03 | **IP sources** — one hit per source (sub = target) → `#/settings/sources` |
| SRCH-04 | **Executions** — the newest 30 runs (label = task · time, sub = message / diff) → opens the run's detail modal |
| SRCH-05 | **Pages** — every nav destination |
| SRCH-06 | Keyboard: `↑`/`↓` move the highlight, `Enter` opens it, `Esc` dismisses; clicking a hit works even though the input loses focus first |
| SRCH-07 | Empty query → no panel; no match → "Nothing matches "{q}"." |
| SRCH-08 | `/` focuses the box from anywhere (except inside a text field) |
| SRCH-09 | Works offline against the bundled mock dataset |

---

## 9. Test-data design principles

- **Documentation addresses only.** Use `203.0.113.0/24` (TEST-NET-3), `198.51.100.0/24`
  (TEST-NET-2) and `2001:db8::/32` for all IPs — never real public addresses.
- **Obviously-fake secrets.** Tokens look like keys (`cf_••••7f2a`, `oz_tok_…`) but are
  non-functional; long-lived fixture tokens are prefixed `TEST_`.
- **Stable ids, relative times.** Ids (`p-cf`, `j-home-v4`, `r-94`) never change; all
  timestamps are computed as *now − N minutes* so the fixture always looks "live".
- **Every status bucket is represented** so filters, badges and empty states can be
  exercised without creating data by hand.
- **IDN + non-ASCII** are present (`ozhao.cn`, subdomains, `默认` record line) to catch
  punycode/locale bugs early.
- **Counts are assertable**: 6 providers (4 connected, 1 attention, 1 error), 7 tasks
  (6 enabled, 1 disabled), 22 runs. Tests assert these numbers.

## 10. Where each case is exercised

| Layer | Tool | Fixtures |
|---|---|---|
| Provider adapters | `go test ./internal/provider/...` (httptest servers) | REC-01…12, PRV-02/03 |
| Reconciler engine | `go test ./internal/engine/...` | REC-*, SCH-* |
| Repository / storage | `go test ./internal/storage/...` | all seed rows above |
| REST API | `go test ./internal/server/...` | the full fixture set |
| CLI | `go test ./cmd/...` + golden files | SCH-04/05, PRV-01, SET-06 |
| Panel (e2e) | Playwright over the embedded UI | every case ID |
