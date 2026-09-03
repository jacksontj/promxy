# Security

Promxy has two distinct security surfaces: **inbound** (clients talking to
promxy) and **outbound** (promxy talking to your Prometheus servers). They are
configured in different places.

## Inbound: serving promxy over TLS

Promxy uses the standard Prometheus
[web configuration file](https://prometheus.io/docs/prometheus/latest/configuration/https/),
passed via `--web.config.file`. The flag is marked experimental.

```
promxy --config=config.yaml --web.config.file=web.yml
```

```yaml
# web.yml
tls_server_config:
  cert_file: server.crt
  key_file: server.key
  # optional mTLS
  client_auth_type: RequireAndVerifyClientCert
  client_ca_file: test-ca.crt
```

Relative `cert_file` / `key_file` paths resolve against the directory containing
the web config file.

The same file also carries:

```yaml
http_server_config:
  headers:
    Content-Security-Policy: "default-src 'self';"
    X-Frame-Options: "sameorigin"

basic_auth_users:
  alice: $2y$10$...   # bcrypt hash
```

The file is validated at startup, so a broken web config fails fast rather than
serving plaintext.

> A `tls_server_config` key in promxy's **main** config file is accepted by the
> parser but has no effect. TLS comes from `--web.config.file` only.

### Legacy flat schema

Before promxy delegated its web server to `exporter-toolkit`, TLS keys lived at
the *top level* of the web config file rather than nested under
`tls_server_config`:

```yaml
# deprecated
cert_file: server.crt
key_file: server.key
```

Promxy still serves this schema for backward compatibility, logging a
deprecation warning at startup. Support will be removed in a future release —
nest the keys under `tls_server_config:`.

## Inbound: authentication and authorization

**Promxy provides no authentication or authorization of its own** beyond the
`basic_auth_users` supported by the web config file. There is no per-tenant
access control, and nothing in promxy restricts which series a caller may query.

For anything more than basic auth, put a proxy in front of promxy:

- **Kubernetes ingress** — e.g. nginx ingress with an auth annotation
- **A reverse proxy** — nginx/Envoy doing auth, as in the
  [Prometheus basic-auth guide](https://prometheus.io/docs/guides/basic-auth/)
- **[prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy)**
  — enforces a *trusted* label matcher on every query, which is the right tool
  for real tenant isolation

### Why `label_filter` and `inject_matchers` are not security features

Both operate on the query's matchers, and both trust the downstream to honour
them:

- `label_filter` decides whether to *send* a query based on its matchers. A
  caller who matches on a different label bypasses the filter.
- `inject_matchers` *adds* matchers to the query, but a caller who controls the
  query text can construct expressions that work around them.

They are performance and scoping tools. Use a trusted-label proxy for isolation.
See [Multi-tenancy](multi-tenancy.md).

### CORS

`--web.cors.origin` (default `.*`) is a fully-anchored regex for allowed
origins. The default allows any origin; tighten it if promxy is reachable from a
browser context you don't control.

## Outbound: authenticating to downstreams

Per server group, `http_client` inlines Prometheus'
[HTTP client config](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_config),
plus a `sigv4` block for AWS-signed requests (Amazon Managed Prometheus):

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prom-01:9090]
      scheme: https
      http_client:
        tls_config:
          ca_file: /etc/ssl/certs/internal-ca.crt
          cert_file: /etc/promxy/client.crt
          key_file: /etc/promxy/client.key
        basic_auth:
          username: promxy
          password_file: /etc/promxy/password
```

**At most one** authentication method may be configured per group —
`basic_auth`, `authorization`, `bearer_token`, `bearer_token_file`, or `sigv4`.
Configuring more than one is a config error caught at load time.

Prefer the `*_file` variants (`password_file`, `bearer_token_file`) so secrets
aren't sitting in the config file that promxy serves back from
`/api/v1/status/config`.

### Forwarding caller credentials

`--proxy-headers` forwards named headers from the incoming request to
downstream server groups:

```
promxy --proxy-headers=Authorization --proxy-headers=X-Scope-OrgID
```

It can also be set via the `PROXY_HEADERS` environment variable. This lets an
authenticating layer in front of promxy pass a caller's identity through to the
backends, so the backends can enforce it.

### Static headers

For a fixed credential per group — the common case for Mimir/Cortex tenancy —
use `http_headers`:

```yaml
http_headers:
  X-Scope-OrgID: tenant-A
```

## Exposed endpoints

Promxy exposes the full Prometheus UI and API, plus `/debug/pprof/*`. If promxy
is reachable beyond your trusted network, restrict `/debug` at your proxy —
pprof endpoints are a denial-of-service and information-disclosure surface.

`--web.enable-lifecycle` adds `POST /-/reload`. Leave it off unless something
needs it, and restrict it if on.

`/api/v1/status/config` returns promxy's configuration, including any secrets
inlined in the config file. This is another reason to prefer `*_file` secret
references.

## Container

The published image runs as `nobody`. If you build your own, keep it that way —
promxy needs no privileges.
