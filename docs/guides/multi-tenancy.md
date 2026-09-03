# Multi-tenancy

Multi-tenant backends (Mimir, Cortex, GEM) identify a dataset by
`(endpoint, tenant)`, not endpoint alone. Since a server group is "endpoints
holding the **same** data", the unit that maps onto a group is the
`(backend, tenant)` pair.

Get this wrong and it fails quietly: promxy merges datasets it thinks are
replicas, and `count(up)` comes back under-counted.

## One group per tenant

Select the tenant with `X-Scope-OrgID`, and give each group a static label so
its results stay distinguishable:

```yaml
promxy:
  server_groups:
    - http_headers:
        X-Scope-OrgID: dc1
      path_prefix: /prometheus
      scheme: https
      static_configs:
        - targets: [mimir.dc1.local:8080]
      labels:
        dc: dc1

    - http_headers:
        X-Scope-OrgID: dc2
      path_prefix: /prometheus
      scheme: https
      static_configs:
        - targets: [mimir.dc2.local:8080]
      labels:
        dc: dc2
```

**Do not** list several tenants in one `X-Scope-OrgID` header to save groups.
The backend returns the union, but promxy sees one dataset and merges within it,
so replicas across tenants collapse and `count(up)` is wrong. See
[issue #703](https://github.com/jacksontj/promxy/issues/703).

## Replicated tenants

When the *same* tenant is served by multiple backends, that is one dataset with
HA replicas, so those endpoints belong in one group:

```yaml
    - http_headers:
        X-Scope-OrgID: google-us-dc1
      path_prefix: /prometheus
      scheme: https
      static_configs:
        - targets:
            - mimir.dc1.local:8080
            - mimir.dc2.local:8080
      labels:
        dc: google-us-dc1
```

A full worked topology:
[`cmd/promxy/multi_tenant.conf`](../../cmd/promxy/multi_tenant.conf).

## Slicing one backend with `inject_matchers`

The other shape: a single backend (big Prometheus, Thanos, single-tenant Mimir)
holds many logical clusters distinguished by a label, and you want a per-tenant
view.

`inject_matchers` adds matchers to **every** selector sent to a group, including
queries that never mention the label. With `cluster="A"` configured, `count(up)`
goes downstream as `count(up{cluster="A"})`.

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [big-prometheus:9090]
      inject_matchers:
        - 'cluster="A"'
```

Each entry is one matcher in PromQL syntax without braces; regex matchers work
(`'region=~"us-.*"'`). Matchers are validated at config load, so a syntax error
fails startup rather than a later discovery sync.

Usual deployment: one promxy per tenant in front of a shared backend. See
[issue #698](https://github.com/jacksontj/promxy/issues/698).

| Option | What it does |
| ------ | ------------ |
| `labels` | Adds labels to *responses*. Doesn't change the query. |
| `label_filter` | *Skips* a downstream that can't match. Doesn't change the query. |
| `inject_matchers` | Adds matchers to the query itself. |

## Security

Neither `inject_matchers` nor `label_filter` is a security boundary: both
manipulate query matchers and rely on the downstream honouring them, and a
caller who controls the query text can work around them.

For real tenant isolation put an authenticating proxy in front that injects a
*trusted* label set, e.g.
[prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy).
See [Security](security.md).
