# OzDDNS

English | [简体中文](README_ZH.md)

OzDDNS is a provider-centric dynamic DNS manager for keeping DNS records aligned
with addresses discovered from HTTP services, local interfaces, commands, or
static values.

It ships as a single Go binary with an embedded Vue web panel, a REST API, a
management CLI, a scheduler, and SQLite storage. The shared reconciliation
engine reads the current DNS record, computes a diff, and writes only when the
desired state differs.

> OzDDNS is under active development. Review the [security notes](#security)
> and [license status](#license) before exposing or redistributing it.

## Highlights

- **Reconciliation first**: inspect current and desired values before writing.

- **Safe previews**: dry-run a task from the web panel, CLI, or REST API.

- **Provider-centric configuration**: configure multiple accounts independently
  and reuse them across tasks.

- **Flexible address sources**: HTTP lookup, network interface, shell command,
  or static IPv4/IPv6 address.

- **One operational surface**: web panel, CLI, and API call the same application
  layer.

- **Built-in automation**: per-task intervals, optional jitter, concurrent
  workers, pause/resume, run-all, and rolling execution history.

- **Operations included**: notification webhooks, configuration backup/restore,
  optional password + TOTP authorization, health checks, and structured JSON
  CLI output.

- **Single-binary delivery**: the production web bundle is embedded with
  `go:embed`.

- **Localized panel**: English, Simplified Chinese, and Japanese; light, dark,
  and system themes.

## Supported providers

| Provider             | Type         |
| -------------------- | ------------ |
| Cloudflare           | `cloudflare` |
| Alibaba Cloud DNS    | `alidns`     |
| DNSPod (Tencent)     | `dnspod`     |
| DuckDNS              | `duckdns`    |
| Namecheap            | `namecheap`  |
| GoDaddy              | `godaddy`    |
| deSEC                | `desec`      |
| Vercel DNS           | `vercel`     |
| Custom HTTP endpoint | `custom`     |

OzDDNS manages IPv4 `A` and IPv6 `AAAA` targets where supported by the selected
provider. Cloudflare proxy state is supported for Cloudflare records.

## Quick start

### Build from source

Requirements:

- Go 1.26 or later

- Node.js 22 or later and npm

- GNU Make

```bash
git clone https://github.com/echoface/ozddns.git
cd ozddns
make build

./bin/ozddns --db ./ozddns.db serve
```

Open <http://127.0.0.1:9876>.

To explore the interface with sample providers, tasks, and execution history,
start a new database with the demo dataset:

```bash
./bin/ozddns --db ./demo.db serve --demo
```

`make build` installs the locked frontend dependencies, builds the Vue panel,
embeds it, and produces `./bin/ozddns`.

### Run the published image

Release images are published to GitHub Container Registry for `linux/amd64` and
`linux/arm64`:

```bash
docker volume create ozddns-data

docker run -d \
  --name ozddns \
  --restart unless-stopped \
  -p 127.0.0.1:9876:9876 \
  -v ozddns-data:/data \
  ghcr.io/echoface/ozddns:latest \
  --db /data/ozddns.db serve --listen 0.0.0.0:9876
```

Use a version tag such as `ghcr.io/echoface/ozddns:1.1.0` when reproducible
deployments are required.

### Docker Compose

```bash
docker compose up -d          # pulls ghcr.io/echoface/ozddns:latest
docker compose up -d --build  # or build the same image locally
```

The included Compose configuration:

- runs the published image `ghcr.io/echoface/ozddns:latest`, or any tag you pass
  as `OZDDNS_IMAGE` (e.g. `OZDDNS_IMAGE=ghcr.io/echoface/ozddns:1.1.0`);

- uses **host networking** so `interface` IP sources can read the host's NICs
  (a bridged container only sees its own interfaces). There is no port mapping
  in this mode — the panel is bound by `OZDDNS_LISTEN`, defaulting to
  `127.0.0.1:9876`; set it to `0.0.0.0:9876` to expose the panel;

- on Docker Desktop (macOS/Windows) drop `network_mode`, restore the `ports`
  mapping and set `OZDDNS_LISTEN=0.0.0.0:9876`; interface sources then only see
  the container's interfaces;

- stores the SQLite database under `./data`;

- writes a default `config.yaml` to the data volume on first start
  (`./data/config.yaml`) that you can edit before the next restart;

- runs the service as a non-root user inside the container;

- includes an HTTP health check.

Follow the logs with:

```bash
docker compose logs -f
```

### Deployment configuration file

How the process runs — the bind address, the database path, the log level — is
*deployment* configuration. It is resolved once at start-up and is never stored
in the database or edited from the panel (changing the bind address from the
panel would drop the very connection you are using).

Set it with an optional `config.yaml` next to the binary:

```yaml
# config.yaml
listen: 127.0.0.1:9876    # keep loopback unless a trusted proxy fronts the daemon
db: /data/ozddns.db
logLevel: info
basePath: /                # public prefix, e.g. /ddns behind a sub-path proxy
trustProxy: false          # true only behind a trusted proxy that overwrites forwarding headers
trustedNetwork: false      # true only when the bind is reachable solely on networks you control
```

The file is optional: if it is missing when `serve` starts, OzDDNS writes this
annotated default and keeps going (a read-only directory is not an error). Point
at a different path with `--config` or `OZDDNS_CONFIG`; `--config` also works
after the subcommand, as in `ozddns serve --config /etc/ozddns/config.yaml`.

Every deployment setting follows the same precedence, so the more specific
source always wins: **flag > environment variable > `config.yaml` > built-in
default**. Both the flag and the environment variable are optional; the names
line up as follows.

| Environment variable | Flag | `config.yaml` key | Default | Purpose |
| --- | --- | --- | --- | --- |
| `OZDDNS_LISTEN` | `--listen` | `listen` | `127.0.0.1:9876` | Bind address for the API and panel. |
| `OZDDNS_DB` | `--db` | `db` | `./ozddns.db` | SQLite database path. |
| `OZDDNS_LOG_LEVEL` | `--log-level` | `logLevel` | `info` | `debug`, `info`, `warn`, or `error`. |
| `OZDDNS_BASE_PATH` | `--base-path` | `basePath` | `/` | Public prefix when mounted behind a sub-path reverse proxy. |
| `OZDDNS_TRUST_PROXY` | `--trust-proxy` | `trustProxy` | `false` | Trust `X-Forwarded-Proto` / `X-Forwarded-For` from a proxy that **overwrites** both. |
| `OZDDNS_TRUSTED_NETWORK` | `--trusted-network` | `trustedNetwork` | `false` | Assert the bind is reachable only on networks you control; silences the beyond-loopback exposure warning. |
| `OZDDNS_CONFIG` | `--config` | — | `./config.yaml` | Path to the config file (there is no key for the file's own location). |

`trustProxy` is not a general-purpose "HTTPS is enabled" switch. Enable it only
when every request reaches OzDDNS through a trusted reverse proxy that
**overwrites** both `X-Forwarded-Proto` and `X-Forwarded-For`. It controls the
`Secure` session-cookie flag and also determines the client address used for
rate limiting and loopback-only authorization enrollment. Trusting headers on a
directly reachable listener lets a client forge its source address.

`trustedNetwork` is not a proxy switch either. It is an explicit assertion that
the bind address is reachable only through networks you control — a LAN, or an
overlay such as Tailscale — so an open panel deliberately bound beyond loopback
is not flagged. It removes only the warning and the panel banner; it adds no
access control of its own. Leave it off for any bind the public internet can
reach, and keep the bind, the proxy ACLs, and the network ACLs tight.

`ozddns health` probes the running daemon's `/healthz` endpoint using the same
resolution, so container and script health checks never guess a port.

`ozddns serve` is the daemon: one long-running process that owns the HTTP
server, scheduler, reconciliation work, and execution-history retention. There
is no second OzDDNS supervisor process. systemd, Docker, or another process
manager may supervise that single process.

### Reverse proxy (nginx)

OzDDNS does not terminate TLS. Run it behind nginx, leave the daemon bound to
loopback, and terminate HTTPS at the proxy. The panel works either on a
dedicated domain or under a sub-path of a shared one:

| Topology | Public URL | Start-up setting |
| --- | --- | --- |
| Dedicated domain | `https://ddns.example.com/` | default |
| Sub-path | `https://app.example.com/ddns/` | `--base-path /ddns` |

The mount point is a start-up setting, so one image serves both — nothing is
rebuilt per deployment:

```bash
ozddns serve --base-path /ddns      # or OZDDNS_BASE_PATH=/ddns, or basePath in config.yaml
```

The daemon strips the prefix itself, so the proxy forwards requests untouched
and needs no rewriting. Copy-ready server blocks, the full walkthrough, and
troubleshooting notes are in
[Running OzDDNS behind nginx](docs/nginx.md):

- [nginx/ozddns-dedicated-domain.conf](docs/nginx/ozddns-dedicated-domain.conf)

- [nginx/ozddns-subpath.conf](docs/nginx/ozddns-subpath.conf)

## First configuration

The web panel is the shortest setup path:

1. Add and validate a DNS provider.
2. Add an IP source.
3. Create a sync task and its DNS targets.
4. Preview the reconciliation diff.
5. Run it once, then leave the scheduler enabled.
6. Enable authorization before exposing the panel beyond loopback.

### Access modes

- **Local open mode (default)** — the daemon listens on `127.0.0.1:9876` and
  accepts API requests without credentials. This is the intended mode for a
  trusted single-user machine.
- **Authorized mode** — enable it in **Settings → Security**. Setup creates one
  administrator with a password, a TOTP authenticator, and ten one-time recovery
  codes. Sign-in is password → TOTP/recovery code → server-side session.

Initial enrollment is accepted only from a loopback client so an exposed,
still-open instance cannot be claimed remotely. Perform the first setup locally,
then configure the reverse proxy and expose the authorized instance.

Authorization can be turned off from the panel after re-verifying the password
and TOTP/recovery code. That deletes the administrator, all sessions, the TOTP
secret, and every recovery code. Enabling it again starts a completely new
enrollment with a new TOTP secret.

Authorization protects the HTTP management surface only. The scheduler,
reconciler, and notification delivery keep running while a browser is signed
out. OzDDNS currently has one administrator and no separate API-token or role
model; a script calling a protected endpoint must complete the same login
handshake and retain the `ozddns_session` cookie.

If both the authenticator and recovery codes are lost, use the local break-glass
command on the machine that owns the database:

```bash
./bin/ozddns --db ./ozddns.db auth disable --yes
```

The provider → IP source → task workflow below is also available from the CLI.
Authorization enrollment itself is intentionally local and panel-only; the CLI
exposes `auth status` and the filesystem-protected `auth disable --yes`
break-glass path.

```bash
./bin/ozddns --db ./ozddns.db provider add \
  --type cloudflare \
  --name "Home Cloudflare" \
  --set api_token="$CF_API_TOKEN"

./bin/ozddns --db ./ozddns.db source add \
  --name "Public IPv4" \
  --type http \
  --family v4 \
  --target https://api.ipify.org

./bin/ozddns --db ./ozddns.db job add \
  --name "Home IPv4" \
  --provider "Home Cloudflare" \
  --source "Public IPv4" \
  --target home.example.com:A:300 \
  --jitter

./bin/ozddns --db ./ozddns.db job run --dry-run <job-id>
./bin/ozddns --db ./ozddns.db job run <job-id>
```

Use `ozddns --help` or `ozddns <command> --help` for the complete CLI reference.
Add `--json` before a command when machine-readable output is supported.

## Configuration backup

Export and restore providers, IP sources, tasks, and settings:

```bash
./bin/ozddns --db ./ozddns.db config export --out ozddns-config.json
./bin/ozddns --db ./ozddns.db config import --replace ozddns-config.json
```

The web panel provides the same operations under **Settings → Storage**.
Execution history is not part of the configuration bundle.

> Configuration exports contain provider credentials so that they can restore a
> working instance. Authorization state, password hashes, TOTP secrets, and
> recovery codes are excluded. Protect exported files like secrets.

## Architecture

```text
Web panel ─┐
REST API ──┼── application services ── scheduler / reconciler
CLI ───────┘              │                    │
                          ├── SQLite            ├── IP sources
                          └── settings           └── DNS providers
```

The main packages are:

```text
cmd/ozddns/          single binary entry point
internal/app/        use cases shared by web, API, and CLI
internal/auth/       password, TOTP, recovery-code, and token primitives
internal/engine/     scheduler and reconciliation diff
internal/provider/   provider contract and built-in adapters
internal/ipsource/   HTTP, interface, command, and static sources
internal/storage/    repository contracts and SQLite implementation
internal/server/     REST API and HTTP middleware
scripts/             isolated browser-level end-to-end checks
web/v2/              Vue 3, TypeScript, and Vite panel
web/                 embedded production assets
```

More detail:

- [Design and architecture](docs/01-design.md)

- [REST API reference](docs/api_references.md)

- [Running OzDDNS behind nginx](docs/nginx.md)

## Extending providers

A provider adapter implements `provider.Factory` and `provider.Provider`, then
registers one factory in `internal/providers/all.go`. Provider metadata describes
its configuration fields, allowing the panel to render the form without
provider-specific UI code.

The reconciliation engine remains provider-independent: adapters validate
credentials, list zones and records, and apply changes; the engine owns address
resolution, diff calculation, scheduling, and run history.

See `internal/provider/custom/custom.go` for the smallest built-in adapter.

## Security

- The bind address is deployment configuration, not a stored setting. It is
  resolved at start-up from `--listen`, `OZDDNS_LISTEN`, or `config.yaml`
  (default `127.0.0.1:9876`); the panel only shows it.

- Authorization is opt-in. Open mode is safe only while the listener is bound to
  loopback; the daemon warns and the panel shows a banner when an open instance
  is reachable from another host.

- `--trusted-network` / `OZDDNS_TRUSTED_NETWORK` / `trustedNetwork` silences that
  warning for a bind you assert is reachable only on networks you control — a
  LAN, or an overlay such as Tailscale. It changes no access control: do not set
  it for a bind the public internet can reach.

- First authorization enrollment is accepted only from a loopback client. If a
  reverse proxy is in front of the daemon, it must overwrite forwarding headers
  and OzDDNS must be started with `--trust-proxy`; otherwise the daemon cannot
  distinguish a remote client from the local proxy.

- Authorized mode uses a PBKDF2-SHA256 password hash, RFC 6238 TOTP, single-use
  recovery codes, and opaque server-side sessions. The browser receives only an
  `HttpOnly`, `SameSite=Lax` cookie; the database stores token hashes.

- Session lifetime is 30 minutes of inactivity and 12 hours absolute. Turning
  authorization off invalidates every session immediately.

- The current security model is single-administrator. There are no roles,
  per-user permissions, or standalone API tokens. Login, MFA, and sensitive
  actions use bounded per-source-IP token buckets in the single OzDDNS process;
  the limiter retains at most 8192 counters and evicts expired/old entries.

- Provider secrets are masked in normal API, CLI, and panel reads.

- Provider credentials are stored in the SQLite database and configuration
  exports. Protect both with filesystem permissions and backups appropriate for
  secrets.

- OzDDNS does not terminate TLS. Use a trusted reverse proxy for HTTPS before
  exposing it to another network.

- The command IP source executes a local shell command. Only trusted
  administrators should be allowed to configure it.

## Development

```bash
make web-v2      # build only the web panel
make build       # build the panel and single binary
make test        # run the Go test suite
make test-race   # run tests with the race detector
make vet         # run go vet
make cover       # write coverage.out
make docker      # build the container image
make hooks       # install the Git hooks for this clone
```

Frontend development:

```bash
cd web/v2
npm ci
npm run dev
```

The authorization and protected-panel contract is also covered by a real
Headless Chrome/CDP test. Start a fresh OzDDNS instance and an isolated headless
Chromium with a CDP endpoint, then run:

```bash
OZDDNS_E2E_URL=http://127.0.0.1:9876 \
OZDDNS_CDP_URL=http://127.0.0.1:9222 \
node scripts/e2e-auth-cdp.mjs
```

The script drives the built panel, not the user's normal browser session. It
checks open local mode, health access, TOTP enrollment, recovery codes, session
reload, offline sign-out safety, API gating, TOTP/recovery-code replay
rejection, authorization disable/re-enable, session invalidation, primary
routes, and desktop/mobile overflow.

The generated `web/v2/dist` directory is intentionally committed because
`go:embed`, `go install`, and direct Go tests require an embeddable panel to be
present.

A version-controlled `pre-push` hook keeps that committed panel in sync: it
rebuilds `web/v2` and refuses the push when `web/v2/dist` no longer matches the
sources. Enable it once per clone:

```bash
make hooks
```

Push with `--no-verify` to bypass it for a single push.

### Publishing releases

The `Publish container image` GitHub Actions workflow validates container builds
on pull requests and publishes multi-platform images to
`ghcr.io/echoface/ozddns`:

- branch pushes do not publish images;

- a Git tag such as `v1.2.3` publishes `v1.2.3`, `1.2.3`, `1.2`, and `latest`.

Create a release image with:

```bash
git tag v1.0.0
git push origin v1.0.0
```

No registry secret is required; the workflow publishes with GitHub's scoped
`GITHUB_TOKEN`. After the first successful run, open the package settings on
GitHub and change its visibility to **Public** if anonymous pulls are required.
GitHub warns that this visibility change cannot be reversed.

## Acknowledgements

OzDDNS was informed by source-level study of two established DDNS projects:

- [qdm12/ddns-updater](https://github.com/qdm12/ddns-updater) demonstrates a
  broad provider ecosystem, periodic DNS reconciliation, a web interface, and
  practical container delivery.

- [jeessy2/ddns-go](https://github.com/jeessy2/ddns-go) demonstrates a
  user-friendly embedded panel, multiple address acquisition methods, webhook
  integration, and straightforward self-hosted deployment.

OzDDNS is an independent implementation. It adopts the proven separation between
address discovery and DNS updates, while using a provider registry, normalized
SQLite storage, and a shared diff engine.

The project also depends on and is made possible by:

- [Go](https://go.dev/) and its standard library;

- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), the CGo-free
  SQLite driver used by the backend;

- [Vue](https://vuejs.org/) and
  [Vue Router](https://router.vuejs.org/) for the management panel;

- [Vite](https://vite.dev/) and
  [TypeScript](https://www.typescriptlang.org/) for frontend development and
  production builds;

- [SQLite](https://www.sqlite.org/) for durable local storage.

Thanks to the maintainers and contributors of these projects. Their names and
trademarks remain the property of their respective owners.

## License

This repository does not currently include a project license. Source
availability alone does not grant permission to use, modify, or redistribute
the project. A license must be selected and added before treating OzDDNS as an
open-source release.
