# Troubleshooting

## Reading promxy's errors

Promxy annotates errors as they propagate up the client stack, so a downstream
failure names the exact target and the exact group:

```json
{
  "status": "error",
  "errorType": "execution",
  "error": "error in servergroup ord=0: error in target=http://127.0.0.1:19090: Post \"http://127.0.0.1:19090/api/v1/query\": dial tcp 127.0.0.1:19090: connect: connection refused"
}
```

`ord=0` is the server group's index in your config — the same value as the
`ordinal` label on `server_group_targets`. Give your groups a `name:` and it
also shows up in logs and that metric.

`--log-level=debug` logs every downstream request, including the rewritten query
promxy actually sent, which is the fastest way to see what pushdown did.

## Promxy won't start

### It hangs at startup with no error

Most likely a `label_filter` with the default `on_sync_error: abort`. Promxy
blocks until the filter's first sync from the downstream succeeds, retrying
every 5s, so one unreachable downstream stops startup entirely.

Either fix the downstream, or choose an explicit failure mode:

```yaml
label_filter:
  on_sync_error: open    # serve unfiltered until the first sync lands
```

See [Label filtering](../guides/label-filtering.md#startup-behaviour-on_sync_error).

### `promxy doesn't support recording rules`

Promxy has no local TSDB, so recording rules need somewhere to write. Add a
`remote_write` endpoint. See
[Rules and alerting](../guides/rules-and-alerting.md).

### `--storage.path must be set if you wish to enable max query concurrency limits`

`--query.max-concurrency` needs the active query tracker, which is a file on
disk. Either set `--storage.path` or leave concurrency unlimited (`-1`, the
default).

### `--storage.tsdb.path and --storage.path are mutually exclusive`

`--storage.tsdb.path` is a deprecated alias — promxy has no TSDB and the flag is
misnamed. Use `--storage.path` alone.

### `at most one of basic_auth, authorization, bearer_token, bearer_token_file, sigv4 must be configured`

A server group's `http_client` has more than one authentication method. Pick
one.

## Queries fail or return errors

### One dead shard fails every query

That is the default, and it is deliberate: a missing shard means silently
missing data, and promxy would rather error than hand alerting rules a wrong
answer.

To opt a group out, per group:

- `downgrade_error: true` — errors become warnings on the response. Preferred.
- `ignore_error: true` — errors are dropped entirely.

See [HA and merging](../concepts/ha-and-merging.md#availability-vs-correctness).

### Errors mentioning histogram fidelity

A query involving native histograms hit a group with no `remote_read`. Promxy
fails loud rather than serve degraded histogram data.

```yaml
remote_read: true
```

Or accept the loss explicitly with `native_histogram.allow_lossy: true`. See
[Native histograms](../guides/native-histograms.md).

### Queries time out

Check `server_group_request_duration_seconds` by `host` to find the slow
downstream. Promxy's goal is to be no slower than the slowest server it has to
talk to, so a slow query here is usually a slow query *there*.

The relevant knobs are `--query.timeout` (default `2m`) on promxy, and per
group, `timeout` (response headers) and `http_client.dial_timeout` (default
`200ms` — low enough that a slow-to-accept downstream will fail).

If promxy is meaningfully slower than a downstream for the same query, that is
worth [an issue](https://github.com/jacksontj/promxy/issues).

## Wrong data

### `count(up)` and other label-less aggregations are too low

Two server groups are returning data promxy considers duplicates. Either:

- Two groups genuinely hold the same series and lack distinguishing `labels` —
  add a static label per group.
- You put several tenants in one group (e.g. multiple tenants in a single
  `X-Scope-OrgID` header). Model each `(backend, tenant)` pair as its own group.
  See [Multi-tenancy](../guides/multi-tenancy.md).

### `count_over_time` is roughly double

`anti_affinity` is too small, so skewed duplicates from HA replicas are
surviving the merge. Set it to your scrape interval.

If a single group hosts metrics scraped at *different* intervals, no single
value is right — set `anti_affinity_dynamic: true`. See
[HA and merging](../concepts/ha-and-merging.md).

### Series look thinned out / samples are missing

`anti_affinity` is too large: legitimately-spaced samples are being treated as
duplicates and dropped. It should be your scrape interval, not a multiple of it.

### HA replicas aren't being merged at all

Your Prometheus servers are probably adding a distinguishing external label
(`replica`, `prometheus_replica`). Different label sets means different series,
so there is nothing to merge. Drop it in the query path:

```yaml
metrics_relabel_configs:
  - action: labeldrop
    source_label: replica
```

### A Mimir/Cortex group returns nothing for `query_range`

Mimir and Cortex snap `query_range` output to the epoch step grid. If the
request start isn't a multiple of the step, the returned samples can sit far
enough off promxy's grid that the lookback delta doesn't reach them.

```yaml
align_query_range_with_step: true
```

Set this only for backends that actually step-align — vanilla Prometheus does
not. See [issue #787](https://github.com/jacksontj/promxy/issues/787).

### A group returns nothing and you're using `label_filter`

The filter may be stale or wrong. Check
`promxy_label_filter_filtered_count_total`, and remember that
`static_labels_exclude` is applied **last**, overriding both dynamic and static
includes.

## Discovery

### A group has zero targets

`server_group_targets{ordinal="N"} == 0`, and promxy logs a warning. Usually
either service discovery isn't returning anything or a `relabel_configs` `keep`
rule is dropping everything. Alert on this — see
[Metrics](metrics.md#server_group_targets).

### Downstreams resolve to an unreachable IPv6 address

```yaml
http_client:
  dial_network: tcp4
```

Alternatively tune the dual-stack fallback with `fallback_delay` — but it must
be **shorter than `dial_timeout`** (default `200ms`) or the dial times out
before the fallback is ever attempted.

## remote_write

### `snappy: decoded length N exceeds limit 33554432`

Batches are too large for the receiver's 32 MiB decompression limit. Promxy
already defaults `max_samples_per_send` to 100 for this reason; if you raised
it, lower it again.

### Samples are lost across restarts

Without `--storage.path` the remote_write WAL lives in a temp directory that is
deleted on shutdown. Set `--storage.path` for a durable WAL.

## Web UI

### The UI is blank or errors

The binary was built without the `builtinassets` tag, which embeds the web
assets:

```
go build -mod=vendor -tags netgo,builtinassets
```

See [Development](../development.md).

### `/-/quit` doesn't stop promxy

It is routed by the embedded Prometheus web handler when
`--web.enable-lifecycle` is set, but promxy doesn't act on it. Use `SIGTERM`.

### TLS deprecation warning at startup

Your `--web.config.file` uses the old flat schema. Nest the keys under
`tls_server_config:` — see [Security](../guides/security.md#legacy-flat-schema).

## Shutdown

### Requests are dropped during rollouts

Promxy fails `/-/ready` and then keeps serving for `--http.shutdown-delay`
(default `10s`) so load balancers can drain. Two things to check:

1. The delay is at least your health-check interval × unhealthy threshold.
2. Your orchestrator's grace period exceeds
   `shutdown-delay + shutdown-timeout`. Kubernetes'
   `terminationGracePeriodSeconds` defaults to 30s, which is shorter than
   promxy's `10s + 60s` defaults.

## Still stuck?

Open an [issue](https://github.com/jacksontj/promxy/issues). Include your
config (with secrets removed), the promxy version (`promxy --version`), the
downstream implementation and version, and debug-level logs for the failing
query.
