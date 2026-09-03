# Multi-tenancy

Multi-tenant backends (Mimir, Cortex, GEM) identify a dataset by
`(endpoint, tenant)` rather than by endpoint alone. Since a promxy server group
is defined as "endpoints holding the **same** data", the unit that maps onto a
server group is the `(backend, tenant)` pair — not the backend.

Getting this wrong is quiet rather than loud: promxy will merge datasets it
believes are replicas, and label-less aggregations like `count(up)` come back
under-counted.

## One group per tenant

Select the tenant with the `X-Scope-OrgID` header, and give each group a static
label so its results stay distinguishable:

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

**Do not** list several tenants in one `X-Scope-OrgID` header to save on groups.
The backend will happily return the union, but promxy sees one dataset and
merges within it — so replicas across tenants collapse and `count(up)` is wrong.
See [issue #703](https://github.com/jacksontj/promxy/issues/703).

## Replicated tenants

When the *same* tenant is served by multiple backends, that genuinely is one
dataset with HA replicas — so those endpoints belong in the same group, and
promxy's anti-affinity merge does the right thing:

```yaml
    # this tenant's data lives on both dc1 and dc2
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

A complete worked topology is in
[`cmd/promxy/multi_tenant.conf`](../../cmd/promxy/multi_tenant.conf).

## Slicing a single merged backend with `inject_matchers`

The other shape of this problem: one backend (a big Prometheus, a Thanos, a
single-tenant Mimir) holds many logical clusters distinguished only by a label,
and you want to present a per-tenant view of it.

`inject_matchers` adds matchers to **every** selector in every request sent to a
group — including queries that never mention the label. With `cluster="A"`
configured, `count(up)` goes downstream as `count(up{cluster="A"})`.

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [big-prometheus:9090]
      inject_matchers:
        - 'cluster="A"'
```

Each entry is one matcher in PromQL syntax without enclosing braces; regex
matchers work (`'region=~"us-.*"'`). Matchers are validated at config load, so a
syntax error fails startup rather than a later discovery sync.

The usual deployment is one promxy per tenant, each with its own
`inject_matchers`, in front of a shared backend. See
[issue #698](https://github.com/jacksontj/promxy/issues/698).

### How it differs from the neighbouring options

| Option | What it does |
| ------ | ------------ |
| `labels` | Adds labels to *responses*. Doesn't change what is queried. |
| `label_filter` | *Skips* a downstream whose filter says it can't match. Doesn't change the query sent. |
| `inject_matchers` | Always *adds matchers to the query itself*. |

## Security

Neither `inject_matchers` nor `label_filter` is a security boundary. Both work
by manipulating query matchers, and both rely on the downstream honouring them —
a caller who controls the query text can work around them.

For genuine tenant isolation, put an authenticating proxy in front that injects
a *trusted* label set, e.g.
[prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy).
See [Security](security.md).
