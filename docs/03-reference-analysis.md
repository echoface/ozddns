# Reference analysis — ddns-updater & ddns-go

Both projects were cloned and read at the source level. The goal of this note is to
extract the *provider model* they converge on, and to state clearly where OzDDNS
diverges (and why).

Sources analysed:

- `qdm12/ddns-updater` — Go 1.26, JSON config, ~60 providers.
- `jeessy2/ddns-go` — Go 1.25, YAML config, ~34 providers, embedded web panel.

---

## 1. ddns-updater (qdm12)

### Provider interface

`internal/provider/provider.go` defines one interface every provider satisfies:

```go
type Provider interface {
    String() string
    Domain() string
    Owner() string
    BuildDomainName() string
    HTML() models.HTMLRow
    Proxied() bool
    IPVersion() ipversion.IPVersion
    IPv6Suffix() netip.Prefix
    Update(ctx context.Context, client *http.Client, ip netip.Addr) (newIP netip.Addr, err error)
}
```

Key properties:

- **One method does the work**: `Update` reads the current record, compares and writes.
  The reconciliation logic lives *inside each provider*, not in a shared engine.
- **Settings are parsed per provider** from `json.RawMessage`. Each provider has a tiny
  typed struct and a `validateSettings(...)` function (see `providers/cloudflare/provider.go`).
- **IP fetching is a separate concern**: the `pkg/publicip` package (http / dns / info
  subfetchers) resolves the address and hands it to `Update`.

### Construction — a switch, not a registry

`provider.New(...)` is one large `switch providerName { case constants.Aliyun: ... }`
over ~60 branches. Adding a provider means editing this file **and** the `constants`
package. There is no `Register(...)` extension point.

### Config & storage

- Config is a JSON file `config.json` with a flat `settings: []` array.
- Each entry mixes the provider name, domain, and provider-specific keys
  (`token`, `key`, `secret`, `password`, `zone_identifier`, `proxied`, `ttl`, …).
- History is kept in a separate persistence layer, not the config.

---

## 2. ddns-go (jeessy2)

### Provider interface

`dns/index.go` defines:

```go
type DNS interface {
    Init(dnsConf *config.DnsConfig, ipv4cache *util.IpCache, ipv6cache *util.IpCache)
    AddUpdateDomainRecords() (domains config.Domains)
}
```

Key properties:

- **`Init` captures config + caches; `AddUpdateDomainRecords` does read → compare → write**
  for A and AAAA. Returned `Domains` carry per-domain `UpdateStatus`.
- **IP caching lives in the interface** (`util.IpCache`), so a provider can avoid an API
  call entirely when the address has not changed since the last run.
- Providers embed `config.DNS` (name/id/secret/ext param) and `config.Domains`.

### Construction — again a switch

`dns.RunOnce()` switches over `dc.DNS.Name` across ~34 branches and instantiates the
struct directly (`dnsSelected = &Cloudflare{}`). Same extension cost as ddns-updater.

### IP sources are first-class *inside* the task config

`config.DnsConfig.Ipv4`/`Ipv6` each carry:

```go
GetType      string // "url" | "netInterface" | "cmd"
URL          string
NetInterface string
Cmd          string
Ipv6Reg      string
Domains      []string
```

So "where do I get the IP" is a per-family setting on the task, supporting HTTP lookup,
NIC read, and shell command. Failure handling is explicit: three consecutive failures to
obtain an address flip the domain to `UpdatedFailed` (guards against transient blips).

### Config, panel & delivery

- Config is YAML (`DnsConf`, `User`, `Webhook`), loaded through a cached accessor and
  written back on save; older v5 configs are migrated on load.
- Ships an **embedded web panel** with bcrypt password auth, a "no WAN access unless a
  user is set" rule, webhook notifications, and language switching.
- Single static binary; the panel assets are compiled in.

---

## 3. What both projects agree on (adopt these)

| Concern | Convergent design | OzDDNS takes it as |
|---|---|---|
| Address lookup | separate from DNS writing | ✅ separate `IPSource` |
| Write strategy | read current → diff → write only if changed | ✅ a shared **reconciler** |
| Transient-failure guard | skip single blips, fail after N tries | ✅ configurable retry/threshold |
| Per-provider settings | opaque blob + per-provider validation | ✅ typed config + `Validate` |
| Secrets | never printed | ✅ masked + redacted |
| Delivery | single binary, embedded UI | ✅ `go:embed` panel |

## 4. Where OzDDNS deliberately diverges (and why)

1. **Registry, not switch.** Both projects hard-code provider dispatch in one giant
   switch. OzDDNS registers factories at start-up:

   ```go
   registry.Register(cloudflare.Factory{})   // no central switch to edit
   ```

   Adding a provider becomes "add a package + one `Register` line".

2. **Reconciliation is a shared engine, not per-provider code.** Providers implement
   `ListRecords` / `Apply`; the engine computes the diff. This is what makes "show me the
   diff" and "dry-run" possible in the UI, which neither project can do cleanly.

3. **Normalized storage instead of a flat file.** Both keep config in one JSON/YAML blob.
   OzDDNS separates **provider instances**, **IP sources**, and **sync jobs** into tables
   with `config_json` only for provider-specific fields (see `docs/01-design.md`).

4. **CLI + Web share application services.** Neither project exposes a scriptable CLI over
   the same use-cases the panel uses. OzDDNS routes `ddnsctl` and the REST handlers through
   the same `JobService` / `ProviderService`.

5. **Zone/record listing as a product feature.** Because OzDDNS models `ListRecords`, the
   panel can show "current vs desired" per record — the feature that anchors the UI.

## 5. The resulting provider contract (target for the Go backend)

```go
type ProviderFactory interface {
    Type() string                                  // "cloudflare"
    Meta() ProviderMeta                            // label, fields[], capabilities
    ValidateConfig(raw json.RawMessage) error
    New(raw json.RawMessage) (DNSProvider, error)
}

type DNSProvider interface {
    Validate(ctx context.Context) error
    ListZones(ctx context.Context) ([]Zone, error)
    ListRecords(ctx context.Context, zone string, f RecordFilter) ([]Record, error)
    Apply(ctx context.Context, change Change) error // create | update | delete
}

type IPSource interface {
    Get(ctx context.Context, family Family) (netip.Addr, error) // v4 | v6
}
```

The engine (`internal/engine`) owns: scheduling, IP acquisition, the diff, retries,
cancellation, and writing runs to history. Providers stay thin.
