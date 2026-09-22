# Server groups

A **server group** is a set of Prometheus-API endpoints holding the *same*
data — the standard HA pattern of N servers running an identical config. Promxy
merges and deduplicates *within* a group, and scatter-gathers *across* groups.

> Same data → same group. Different data → different groups.

Different data in one group silently under-counts (promxy dedups samples it
thinks are duplicates); HA replicas in separate groups double-count.

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prometheus-01:9090, prometheus-02:9090]
      anti_affinity: 10s
```

Defaults below are from `servergroup.DefaultConfig`
([`pkg/servergroup/config.go`](../../pkg/servergroup/config.go)).

## Target discovery

### Service discovery

Any Prometheus service-discovery mechanism can be used inline, with the same
markup as a Prometheus scrape config. The targets discovered are the
**Prometheus servers**, not scrape targets.

```yaml
- kubernetes_sd_configs:
    - role: pod
      namespaces:
        names: [monitoring]
```

Discovery is re-synced every 5s.

### `relabel_configs`

Same syntax as Prometheus' scrape `relabel_configs`, applied to this group's
*downstream Prometheus targets*:

1. `keep`/`drop` decide which discovered hosts are in the group.
2. Labels you set are added to every series returned by that target.

```yaml
relabel_configs:
  - source_labels: [__meta_consul_tags]
    regex: '.*,prod,.*'
    action: keep
  - source_labels: [__meta_consul_dc]
    regex: '.+'
    action: replace
    target_label: datacenter
```

The special label `__path_prefix__` sets the per-target path prefix, letting you
derive `path_prefix` from discovery metadata.

### `path_prefix`

Prefix prepended to all request paths for this group. For servers behind a
path-routing reverse proxy, or Mimir/Cortex (`/prometheus`).

```yaml
path_prefix: /example/prefix
```

### `scheme`

`http` (default) or `https`.

### `name`

Optional human-readable identifier, shown in logs and as the `name` label on
`server_group_targets`. The ordinal is always included regardless.

```yaml
name: primary-cluster
```

## Merging and result shaping

### `anti_affinity`

*Default: `10s`.*

How large a gap must be before promxy fills it from another host in the group.
Set it to your scrape interval. See
[HA and merging](../concepts/ha-and-merging.md).

```yaml
anti_affinity: 10s
```

### `anti_affinity_dynamic`

*Default: `false`.*

Infer the buffer per-series from the data's inter-sample spacing (half the
median gap), falling back to `anti_affinity` when there are too few samples.
Set this when one group hosts metrics scraped at *different* intervals, where no
single static value fits. See
[issue #734](https://github.com/jacksontj/promxy/issues/734).

### `prefer_max`

*Default: `false`.*

When two hosts have a sample at the same timestamp, take the larger value.

### `labels`

Labels added to every series returned from this group. Recommended whenever you
have more than one group, so `count(up)` and friends stay attributable.

```yaml
labels:
  sg: localhost_9090
```

### `metrics_relabel_configs`

Rewrites the labels of *returned series*. These run in the query path, so every
action must be **reversible** — promxy rewrites the incoming query's matchers to
match. Supported actions:

| Action      | Effect |
| ----------- | ------ |
| `labeldrop` | Drop `source_label` from results |
| `replace`   | Rename `source_label` to `target_label` |
| `lowercase` | Lowercase `source_label` into `target_label` |
| `uppercase` | Uppercase `source_label` into `target_label` |

Rules execute in order and compound.

```yaml
metrics_relabel_configs:
  # drop the `replica` label -> replica deduplication, thanos-style
  - action: labeldrop
    source_label: replica
  # rename job -> scrape_job
  - action: replace
    source_label: job
    target_label: scrape_job
  # lowercase in place
  - action: lowercase
    source_label: branch
    target_label: branch
```

## Query routing

### `remote_read`

*Default: `false`.*

Fetch raw data (matrix selectors like `foo[1h]`) through Prometheus' remote_read
API instead of the JSON HTTP API.

**Pros:** `StaleNaN` markers survive, ~2x faster, and it is the only path that
preserves native histograms losslessly.

**Cons:** "experimental" upstream API; protobuf marshalling on the Prometheus
side doesn't stream, so the wire payload costs ~2x its in-memory size on the
remote host; and some backends don't implement it (notably VictoriaMetrics).

```yaml
remote_read: true
remote_read_path: api/v1/read   # default
```

### `native_histogram`

How promxy serves native-histogram queries against this group. Omitting the
block means AST-only detection, failing loud when remote_read can't preserve
fidelity.

```yaml
native_histogram:
  metadata_refresh: 5m
  allow_lossy: false
```

- **`metadata_refresh`** *(default: unset/0)* — enables a metric-name → type
  cache built from `/api/v1/metadata`, so queries touching histogram metrics
  *without* calling a histogram-only function (e.g. `rate(my_hist[5m])`) are
  also routed via remote_read. Unset means pure AST detection.
- **`allow_lossy`** *(default: `false`)* — what to do when a histogram-bearing
  query hits this group and `remote_read` is not configured. `false` errors out
  before fan-out; `true` serves the query over the lossy JSON path anyway.

Full explanation: [Native histograms](../guides/native-histograms.md).

### `inject_matchers`

Label matchers injected into **every** selector of every request sent to this
group — including queries that never reference those labels (`count(up)` is sent
downstream as `count(up{cluster="A"})`). Each entry is one matcher in PromQL
syntax, without enclosing braces.

```yaml
inject_matchers:
  - 'cluster="A"'
  - 'region=~"us-.*"'
```

Scopes the group to a subset of a shared downstream; the usual case is a
per-tenant view of one merged backend. See
[Multi-tenancy](../guides/multi-tenancy.md) and
[issue #698](https://github.com/jacksontj/promxy/issues/698).

> **Not a security boundary.** It only mutates request matchers and relies on
> the downstream honouring them.

### `label_filter`

Skips queries whose matchers can't match this downstream, using an in-memory
filter of the labels it actually has. Full reference:
[Label filtering](../guides/label-filtering.md).

```yaml
label_filter:
  dynamic_labels: [__name__, job]
  sync_interval: 5m
  static_labels_include:
    instance: [instance1]
  static_labels_exclude:
    __name__: [up]
  on_sync_error: abort   # abort | open | closed
```

### `relative_time_range` / `absolute_time_range`

What time range this group holds data for, so promxy can skip it for queries
outside that window. Both blocks are optional, as are `start`/`end` individually.

```yaml
# a group holding only the last 3h, excluding the last 1h
relative_time_range:
  start: -3h
  end: -1h
  truncate: false

# a deprecated group that stopped receiving data
absolute_time_range:
  start: '2009-10-10T23:00:00Z'
  end: '2009-10-11T23:00:00Z'
  truncate: true
```

`truncate` clamps a query's range to the group's window rather than skipping the
group when the ranges only partially overlap.

### `query_params`

Query parameters added to all HTTP calls to this downstream, e.g. `nocache=1`
for VictoriaMetrics
([issue #202](https://github.com/jacksontj/promxy/issues/202)).

```yaml
query_params:
  nocache: 1
```

### `http_headers`

HTTP headers added to calls to this downstream. Primarily `X-Scope-OrgID` for
Mimir/Cortex multi-tenancy.

```yaml
http_headers:
  X-Scope-OrgID: tenant-A
```

> When fanning out across several Mimir/Cortex tenants, model each
> `(backend, tenant)` pair as its own group rather than listing multiple tenants
> in one header, or label-less aggregations get under-counted. See
> [Multi-tenancy](../guides/multi-tenancy.md).

### `align_query_range_with_step`

*Default: `false`.*

Declares that this backend snaps `query_range` results to the epoch step grid
(`k*step`), as Mimir and Cortex do. Promxy then re-stamps returned samples onto
the grid implied by the request start (`start + j*step`).

Leave it **off** for backends that don't step-align (vanilla Prometheus); their
samples already sit on the requested grid. Without it, an unaligned request
(`start % step != 0`) whose off-grid distance exceeds the lookback delta yields
no data from a step-aligning backend. See
[issue #787](https://github.com/jacksontj/promxy/issues/787).

## Error handling

By default a failing group **fails the query**: missing data is worse than an
error when alerting depends on it. Two options trade that for availability.

### `ignore_error`

*Default: `false`.* Makes this group's response optional — if it errors and
others don't, the query still succeeds. Errors are hidden entirely.

### `downgrade_error`

*Default: `false`.* Same effect, but errors become **warnings** on the response
instead of being dropped. Prefer this if your clients surface warnings.

## HTTP client

```yaml
timeout: 5s
max_idle_conns: 20000
max_idle_conns_per_host: 1000
idle_conn_timeout: 300s
http_client:
  dial_timeout: 1s
  dial_network: tcp
  fallback_delay: 50ms
  tls_config:
    insecure_skip_verify: true
```

| Option | Default | Meaning |
| ------ | ------- | ------- |
| `timeout` | `0` (none) | Time to wait for response headers after fully writing the request. Does **not** include reading the response body. |
| `max_idle_conns` | `20000` | Max idle connections kept open for the group. |
| `max_idle_conns_per_host` | `1000` | Max idle connections per host. |
| `idle_conn_timeout` | `5m` | How long an idle connection is kept. |
| `http_client.dial_timeout` | `200ms` | Connection establishment timeout. |
| `http_client.dial_network` | `tcp` | `tcp` (dual-stack), `tcp4`, or `tcp6`. Use `tcp4` to work around downstreams whose DNS resolves to an unreachable IPv6 address. |
| `http_client.fallback_delay` | Go default (`300ms`) | RFC 6555 "Happy Eyeballs" delay before trying the other address family. Negative disables the fallback. Must be **shorter than `dial_timeout`** to have any effect. |

`http_client` also inlines Prometheus'
[`http_client_config`](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_config)
(`tls_config`, `basic_auth`, `authorization`, `bearer_token`,
`bearer_token_file`, `proxy_url`, `follow_redirects`, …) plus a `sigv4` block
for AWS-signed requests.

**At most one** auth method per group — `basic_auth`, `authorization`,
`bearer_token`, `bearer_token_file`, or `sigv4`. More than one is a config
error.

## Full example

[`cmd/promxy/config.yaml`](../../cmd/promxy/config.yaml) is an annotated config
exercising essentially every option above.
