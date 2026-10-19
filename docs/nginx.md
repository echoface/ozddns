# Running OzDDNS behind nginx

OzDDNS never terminates TLS. Put it behind a reverse proxy that owns the
certificate, keep the daemon on loopback (`OZDDNS_LISTEN=127.0.0.1:9876`, the
default), and let nginx be the only thing reachable from the network.

Complete the first authorization enrollment locally before exposing the proxied
hostname. OzDDNS accepts enrollment only from a loopback client. If a local
browser reaches the daemon through a reverse proxy, the proxy must overwrite the
forwarding headers and OzDDNS must trust them so the request is still classified
as loopback; a remote client cannot perform first enrollment.

One image serves both topologies. The mount point is a start-up setting, not a
build option, so the published image works at a dedicated domain *and* under a
sub-path without being rebuilt:

| Topology | Public URL | Start-up setting |
| --- | --- | --- |
| Dedicated domain | `https://ddns.example.com/` | default (`basePath: /`) |
| Sub-path of a shared domain | `https://app.example.com/ddns/` | `--base-path /ddns` |

Ready-to-copy server blocks live next to this document:

- [`nginx/ozddns-dedicated-domain.conf`](nginx/ozddns-dedicated-domain.conf)
- [`nginx/ozddns-subpath.conf`](nginx/ozddns-subpath.conf)

## How it works

Three pieces cooperate so that nothing has to be rebuilt per deployment:

1. **The panel is built with relative URLs.** Its assets are referenced as
   `./assets/index-*.js` and its API calls as `./api/v1/...`, so both resolve
   against whatever URL the document was served from.

2. **The daemon owns its prefix.** `--base-path /ddns` makes the daemon strip
   `/ddns` from incoming requests before routing, so
   `/ddns/api/v1/auth/state` is handled by the ordinary
   `/api/v1/auth/state` route. The reverse proxy does no
   rewriting at all.

3. **The daemon tells the panel where it is.** When a base path is set, the
   served `index.html` gets a `<base href="/ddns/">` element, which is what the
   relative URLs in (1) resolve against. With the default `/`, no element is
   injected and the document is byte-identical to the build output.

The panel uses hash routing, so every in-app URL looks like
`https://app.example.com/ddns/#/jobs/42`. The path portion never changes, which
is why a single relative base is enough: there is no history-mode fallback to
configure, and nginx never sees anything after `#`.

## Configuring the mount point

The base path follows the same precedence as every other deployment setting:
flag, then environment, then `config.yaml`, then the built-in default.

```bash
ozddns serve --base-path /ddns            # flag
OZDDNS_BASE_PATH=/ddns ozddns serve       # environment
```

```yaml
# config.yaml
basePath: /ddns
```

`/ddns`, `ddns/` and `/ddns/` are all accepted and normalized to `/ddns`; the
default is `/`. A value that is not rooted, or that contains whitespace, quotes,
angle brackets, backslashes, `?`, `#` or `.`/`..` segments, is rejected at
start-up rather than written into the document.

The resolved value is shown read-only in **Settings → Server**, next to the bind
address, and reported by `ozddns status`.

## Authorization and trusted proxy headers

When authorization is enabled, the session cookie is marked `Secure` if the
daemon sees TLS directly or trusts an HTTPS forwarded scheme. The same trust
decision also lets the daemon use the forwarded client address for rate limits
and loopback-only enrollment.

Enable it only when every request reaches OzDDNS through this proxy:

```bash
ozddns serve --trust-proxy
# or OZDDNS_TRUST_PROXY=true, or trustProxy: true in config.yaml
```

The proxy must **overwrite**, not append to, both headers:

```nginx
proxy_set_header X-Forwarded-For   $remote_addr;
proxy_set_header X-Forwarded-Proto $scheme;
```

Do not enable `trustProxy` when the listener can be reached directly, and do not
forward an untrusted client-supplied `X-Forwarded-For` chain. OzDDNS consumes
the first address in that header; appending with `$proxy_add_x_forwarded_for`
would let a client forge its source address. If another trusted proxy sits in
front of nginx, normalize the address at that layer instead of passing an
untrusted chain through.

## Private binds without authorization

Sometimes the daemon must bind *beyond* loopback and still run without
authorization — for example a Tailscale `100.64.x.x` address that a
containerized proxy reaches over the overlay. The daemon cannot tell a private
overlay from a public bind, so it warns and the panel shows a banner. When the
address is reachable only on networks you control, say so explicitly:

```bash
ozddns serve --trusted-network
# or OZDDNS_TRUSTED_NETWORK=true, or trustedNetwork: true in config.yaml
```

This silences the exposure warning and the panel banner. It adds no access
control of its own: keep the bind, the proxy ACLs, and the network ACLs (for
example Tailscale ACLs) tight, and never set it for a bind the public internet
can reach.

## Verifying a deployment

```bash
BASE=https://app.example.com/ddns   # or https://ddns.example.com

curl -fsS "$BASE/healthz"                             # public liveness
curl -fsS "$BASE/api/v1/auth/state" | head -c 300     # public auth-mode state
curl -fsS "$BASE/" >/dev/null                         # panel document loads
```

For a sub-path mount, also verify the injected base:

```bash
curl -fsS "$BASE/" | grep -o '<base href="[^"]*"'
# <base href="/ddns/">
```

The `<base href>` must match the mount point. A root mount (`basePath: /`) does
not inject a `<base>` element; the built document is served unchanged. Once
authorization is enabled, `/api/v1/status` and the management APIs require a
session cookie, so do not use them as unauthenticated health checks.

## Troubleshooting

- **Blank page; assets 404, or are refused for "MIME type text/html"** — the
  browser asked for `/assets/...` *at the domain root* instead of
  `/prefix/assets/...`, so a sub-path deployment resolved its relative URLs
  from one level too high. That means the daemon did not know its prefix and/or
  the proxy strips the prefix (`proxy_pass http://host:9876/;` — the trailing
  slash) so the daemon never sees the public path. Verify:

  ```bash
  curl -s https://host/prefix/api/v1/auth/state
  curl -s https://host/prefix/ | grep -o '<base href="[^"]*"'   # <base href="/prefix/">
  ```

  If `basePath` is `/`, the container is not getting `OZDDNS_BASE_PATH`. After
  changing a container's environment, recreate it
  (`docker compose up -d --force-recreate ozddns`) — a plain `restart` keeps the
  environment it was created with.

- **`/ddns` without the trailing slash** — the daemon answers with a 301 to
  `/ddns/` itself, so it works whether or not the proxy also redirects. Nothing
  needs to be added to the nginx config for this.

- **Panel loads but every request fails** — look for a `proxy_pass` that adds or
  removes a prefix (`proxy_pass http://127.0.0.1:9876/ddns/;`). The daemon
  expects the public path and strips it itself; rewriting it in the proxy leaves
  the daemon looking for `/ddns/api/v1/...`, which is not a route.

- **Config import fails with `413 Request Entity Too Large`** — raise
  `client_max_body_size` on the `location` that fronts the proxy. OzDDNS also
  enforces an 8 MiB application limit for configuration imports; larger bundles
  are rejected by the daemon itself.

- **Sign-in is requested twice** — the panel's optional TOTP authorization and
  an outer nginx `auth_basic` gate are both enabled. Choose one access-control
  layer; the panel's administrator is configured in *Settings → Security*.

- **Secure session cookie is missing behind TLS** — forward
  `X-Forwarded-Proto: https` from nginx and start OzDDNS with `--trust-proxy`
  (or `trustProxy: true`). Make sure the proxy overwrites
  `X-Forwarded-For` as well. Otherwise the daemon only sees plain HTTP and the
  proxy's loopback address, so it cannot mark the cookie `Secure` or distinguish
  remote clients for enrollment and rate limiting.

- **First enrollment is rejected with `403`** — the browser did not reach the
  daemon from a loopback address. Complete setup directly at
  `http://127.0.0.1:9876`, or configure the trusted proxy headers above and
  restart with `--trust-proxy`.

- **Scripts or tools calling the API directly** — they need the same prefix:
  `https://host/ddns/api/v1/...`. Only the panel's internal URLs are relative.
