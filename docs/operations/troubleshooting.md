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

`ord=0` is the group's index in your config, matching the `ordinal` label on
`server_group_targets`. Setting `name:` on a group adds it to logs and that
metric too.

`--log-level=debug` logs every downstream request including the rewritten query,
the fastest way to see what pushdown did.

## Promxy won't start

### It hangs at startup with no error

Most likely a `label_filter` with the default `on_sync_error: abort`: promxy
blocks until the first sync succeeds, retrying every 5s, so one unreachable
downstream stops startup. Fix the downstream, or pick an explicit failure mode:

```yaml
label_filter:
  on_sync_error: open    # serve unfiltered until the first sync lands
```

See [Label filtering](../guides/label-filtering.md#startup-behaviour-on_sync_error).

### `promxy doesn't support recording rules`

Recording rules need somewhere to write. Add a `remote_write` endpoint — see
[Rules and alerting](../guides/rules-and-alerting.md).

### `--storage.path must be set if you wish to enable max query concurrency limits`

The active query tracker is a file on disk. Set `--storage.path`, or leave
concurrency unlimited (`-1`, the default).

### `--storage.tsdb.path and --storage.path are mutually exclusive`

`--storage.tsdb.path` is a deprecated, misnamed alias. Use `--storage.path`.

### `at most one of basic_auth, authorization, bearer_token, bearer_token_file, sigv4 must be configured`

A server group's `http_client` has more than one authentication method. Pick
one.

## Queries fail or return errors

### One dead shard fails every query

Deliberate: a missing shard means silently missing data, and an error beats a
wrong answer that alerting depends on. To opt a group out:

- `downgrade_error: true` — errors become warnings on the response. Preferred.
- `ignore_error: true` — errors are dropped entirely.

See [HA and merging](../concepts/ha-and-merging.md#availability-vs-correctness).

### Errors mentioning histogram fidelity

A histogram-bearing query hit a group with no `remote_read`.

```yaml
remote_read: true
```

Or accept the loss explicitly with `native_histogram.allow_lossy: true`. See
[Native histograms](../guides/native-histograms.md).

### Queries time out

Check `server_group_request_duration_seconds` by `host` to find the slow
downstream — a slow query here is usually a slow query *there*.

Knobs: `--query.timeout` (default `2m`), and per group `timeout` (response
headers) and `http_client.dial_timeout` (default `200ms`, low enough that a
slow-to-accept downstream fails).

If promxy is meaningfully slower than a downstream for the same query, that is
worth [an issue](https://github.com/jacksontj/promxy/issues).

## Wrong data

### `count(up)` and other label-less aggregations are too low

Promxy is treating data from two groups as duplicates. Either:

- Two groups hold the same series and lack distinguishing `labels` — add a
  static label per group.
- Several tenants are in one group (e.g. multiple tenants in one
  `X-Scope-OrgID` header). Model each `(backend, tenant)` pair as its own group.
  See [Multi-tenancy](../guides/multi-tenancy.md).

### `count_over_time` is roughly double

`anti_affinity` is too small, so skewed duplicates survive the merge. Set it to
your scrape interval, or `anti_affinity_dynamic: true` if the group hosts mixed
scrape intervals. See [HA and merging](../concepts/ha-and-merging.md).

### Series look thinned out / samples are missing

`anti_affinity` is too large, so legitimately-spaced samples are dropped as
duplicates. It should be your scrape interval, not a multiple of it.

### HA replicas aren't being merged at all

Your servers are probably adding a distinguishing external label (`replica`,
`prometheus_replica`). Different label sets are different series, so there is
nothing to merge. Drop it in the query path:

```yaml
metrics_relabel_configs:
  - action: labeldrop
    source_label: replica
```

### A Mimir/Cortex group returns nothing for `query_range`

Mimir and Cortex snap `query_range` output to the epoch step grid. If the
request start isn't a multiple of the step, samples can land far enough off
promxy's grid that the lookback delta doesn't reach them.

```yaml
align_query_range_with_step: true
```

Only for backends that actually step-align; vanilla Prometheus does not. See
[issue #787](https://github.com/jacksontj/promxy/issues/787).

### A group returns nothing and you're using `label_filter`

The filter may be stale or wrong. Check
`promxy_label_filter_filtered_count_total`, and remember that
`static_labels_exclude` is applied **last**, overriding both dynamic and static
includes.

## Discovery

### A group has zero targets

`server_group_targets{ordinal="N"} == 0`, and promxy logs a warning. Usually
service discovery is returning nothing, or a `relabel_configs` `keep` rule is
dropping everything. Alert on this — see
[Metrics](metrics.md#server_group_targets).

### Downstreams resolve to an unreachable IPv6 address

```yaml
http_client:
  dial_network: tcp4
```

Or tune the dual-stack fallback with `fallback_delay`, which must be **shorter
than `dial_timeout`** (default `200ms`) or the dial times out before the
fallback is attempted.

## remote_write

### `snappy: decoded length N exceeds limit 33554432`

Batches exceed the receiver's 32 MiB decompression limit. Promxy defaults
`max_samples_per_send` to 100 for this reason; if you raised it, lower it.

### Samples are lost across restarts

Without `--storage.path` the remote_write WAL lives in a temp directory that is
deleted on shutdown. Set `--storage.path` for a durable WAL.

## Web UI

### The UI is blank or errors

The binary was built without the `builtinassets` tag:

```
go build -mod=vendor -tags netgo,builtinassets
```

See [Development](../development.md).

### `/-/quit` doesn't stop promxy

It is routed when `--web.enable-lifecycle` is set, but promxy doesn't act on
it. Use `SIGTERM`.

### TLS deprecation warning at startup

Your `--web.config.file` uses the old flat schema. Nest the keys under
`tls_server_config:` — see [Security](../guides/security.md#legacy-flat-schema).

## Shutdown

### Requests are dropped during rollouts

Promxy fails `/-/ready` then keeps serving for `--http.shutdown-delay`
(default `10s`) so load balancers can drain. Check both:

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
