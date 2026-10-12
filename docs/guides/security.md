# Security

Two surfaces, configured in different places: **inbound** (clients → promxy) and
**outbound** (promxy → your Prometheus servers).

## Inbound: TLS

Promxy uses the standard Prometheus
[web configuration file](https://prometheus.io/docs/prometheus/latest/configuration/https/)
via `--web.config.file` (marked experimental).

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

http_server_config:
  headers:
    Content-Security-Policy: "default-src 'self';"
    X-Frame-Options: "sameorigin"

basic_auth_users:
  alice: $2y$10$...   # bcrypt hash
```

Relative `cert_file` / `key_file` paths resolve against the web config file's
directory. The file is validated at startup, so a broken config fails fast
rather than serving plaintext.

> A `tls_server_config` key in promxy's **main** config file is parsed but has
> no effect. TLS comes from `--web.config.file` only.

### Legacy flat schema

Before promxy delegated its web server to `exporter-toolkit`, TLS keys lived at
the top level of the web config file:

```yaml
# deprecated
cert_file: server.crt
key_file: server.key
```

Still served for backward compatibility, with a deprecation warning at startup.
Support will be removed in a future release — nest the keys under
`tls_server_config:`.

## Inbound: authentication

**Promxy has no authentication or authorization of its own** beyond
`basic_auth_users` in the web config file. Nothing restricts which series a
caller may query.

For more than basic auth, put a proxy in front:

- **Kubernetes ingress** with an auth annotation
- **nginx/Envoy**, as in the
  [Prometheus basic-auth guide](https://prometheus.io/docs/guides/basic-auth/)
- **[prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy)**
  — enforces a *trusted* label matcher on every query; the right tool for tenant
  isolation

### Why `label_filter` and `inject_matchers` are not security features

Both operate on query matchers and trust the downstream to honour them:

- `label_filter` decides whether to *send* a query. A caller matching on a
  different label bypasses it.
- `inject_matchers` *adds* matchers, but a caller who controls the query text
  can construct expressions around them.

They are performance and scoping tools. See
[Multi-tenancy](multi-tenancy.md).

### CORS

`--web.cors.origin` (default `.*`) is a fully-anchored regex. The default allows
any origin; tighten it if promxy is reachable from a browser context you don't
control.

## Outbound: authenticating to downstreams

Per group, `http_client` inlines Prometheus'
[HTTP client config](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_config),
plus `sigv4` for AWS-signed requests (Amazon Managed Prometheus):

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

**At most one** auth method per group — `basic_auth`, `authorization`,
`bearer_token`, `bearer_token_file`, or `sigv4`. More than one is a config error
at load time.

Prefer the `*_file` variants so secrets aren't inlined in the config file promxy
serves back from `/api/v1/status/config`.

### Forwarding caller credentials

`--proxy-headers` (or `PROXY_HEADERS`) forwards named request headers to
downstream groups, letting an authenticating layer in front of promxy pass a
caller's identity through for the backends to enforce:

```
promxy --proxy-headers=Authorization --proxy-headers=X-Scope-OrgID
```

### Static headers

For a fixed per-group credential, typically Mimir/Cortex tenancy:

```yaml
http_headers:
  X-Scope-OrgID: tenant-A
```

## Exposed endpoints

- `/debug/pprof/*` — a DoS and information-disclosure surface. Restrict at your
  proxy if promxy is reachable beyond your trusted network.
- `/api/v1/status/config` — returns promxy's config, including inlined secrets.
- `/-/reload` — only with `--web.enable-lifecycle`. Leave it off unless needed.

## Container

The published image runs as `nobody`. Keep it that way in your own builds;
promxy needs no privileges.
