# Running OzDDNS behind nginx

OzDDNS never terminates TLS. Put it behind a reverse proxy that owns the
certificate, keep the daemon on loopback (`OZDDNS_LISTEN=127.0.0.1:9876`, the
default), and let nginx be the only thing reachable from the network.

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
   `/ddns` from incoming requests before routing, so `/ddns/api/v1/status` is
   handled by the ordinary `/api/v1/status` route. The reverse proxy does no
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

## Verifying a deployment

```bash
BASE=https://app.example.com/ddns   # or https://ddns.example.com

curl -fsS "$BASE/api/v1/status" | head -c 200        # API reaches the daemon
curl -fsS "$BASE/" | grep -o '<base href="[^"]*"'    # mount point is declared
```

The `basePath` field in the status payload and the `<base href>` in the document
must agree with the URL you mounted. If they do, the browser resolves assets and
API calls to the right place.

## Troubleshooting

- **Blank page, 404s for the assets** — the panel is being served from a URL the
  daemon was not told about. Set `--base-path` (or `OZDDNS_BASE_PATH`) to the
  prefix the proxy mounts, then restart. Check which value is in force with
  `curl -s https://host/ddns/api/v1/status` and look at `basePath`.

- **`/ddns` without the trailing slash** — the daemon answers with a 301 to
  `/ddns/` itself, so it works whether or not the proxy also redirects. Nothing
  needs to be added to the nginx config for this.

- **Panel loads but every request fails** — look for a `proxy_pass` that adds or
  removes a prefix (`proxy_pass http://127.0.0.1:9876/ddns/;`). The daemon
  expects the public path and strips it itself; rewriting it in the proxy leaves
  the daemon looking for `/ddns/api/v1/...`, which is not a route.

- **Config import fails with `413 Request Entity Too Large`** — raise
  `client_max_body_size` on the `location` that fronts the proxy.

- **Sign-in prompt appears twice** — the panel's optional Basic auth and an
  nginx `auth_basic` block are both enabled. Use one of them; the panel's own
  password is set in *Settings → Security*.

- **Scripts or tools calling the API directly** — they need the same prefix:
  `https://host/ddns/api/v1/...`. Only the panel's internal URLs are relative.
