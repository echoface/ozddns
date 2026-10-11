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
  Basic authentication, health checks, and structured JSON CLI output.

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

- publishes the panel on `127.0.0.1:9876`;

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
listen: 0.0.0.0:9876
db: /data/ozddns.db
logLevel: info
```

The file is optional: if it is missing when `serve` starts, OzDDNS writes this
annotated default and keeps going (a read-only directory is not an error). Point
at a different path with `--config` or `OZDDNS_CONFIG`; `--config` also works
after the subcommand, as in `ozddns serve --config /etc/ozddns/config.yaml`.

Every deployment setting follows the same precedence, so the more specific
source always wins:

- **`--listen` / `--db` / `--log-level` flags** (highest)
- **environment variables** — `OZDDNS_LISTEN`, `OZDDNS_DB`, `OZDDNS_LOG_LEVEL`
- **`config.yaml`** — path taken from `--config` or `OZDDNS_CONFIG` (default `./config.yaml`)
- **built-in defaults** — `127.0.0.1:9876`, `./ozddns.db`, `info` (lowest)

`ozddns health` probes the running daemon's `/healthz` endpoint using the same
resolution, so container and script health checks never guess a port.

## First configuration

The web panel is the shortest setup path:

1. Add and validate a DNS provider.
2. Add an IP source.
3. Create a sync task and its DNS targets.
4. Preview the reconciliation diff.
5. Run it once, then leave the scheduler enabled.
6. Set an administrator password before exposing the panel beyond loopback.

The same flow is available from the CLI:

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
> working instance. The panel password hash is excluded. Protect exported files
> like secrets.

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
internal/engine/     scheduler and reconciliation diff
internal/provider/   provider contract and built-in adapters
internal/ipsource/   HTTP, interface, command, and static sources
internal/storage/    repository contracts and SQLite implementation
internal/server/     REST API and HTTP middleware
web/v2/              Vue 3, TypeScript, and Vite panel
web/                 embedded production assets
```

More detail:

- [Design and architecture](docs/01-design.md)

- [REST API reference](docs/api_references.md)

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

- HTTP Basic authentication is enabled after an administrator password is set.

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
```

Frontend development:

```bash
cd web/v2
npm ci
npm run dev
```

The generated `web/v2/dist` directory is intentionally committed because
`go:embed`, `go install`, and direct Go tests require an embeddable panel to be
present.

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
