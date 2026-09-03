# Label filtering

By default promxy scatter-gathers every query to every server group. That is
usually fine — the fan-out is cheap and the downstreams answer in parallel — but
when you have many shards and most queries only concern one of them, you are
paying for a lot of requests that can only ever return nothing.

`label_filter` maintains an in-memory picture of which label values a downstream
actually has, and skips groups whose filter proves the query can't match.

> **This is not a security mechanism.** It works entirely from the query's
> matchers, so a caller can trivially sidestep it by matching on a different
> label. See [Security](security.md) for real isolation.

## Configuration

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prom-us-01:9090]
      label_filter:
        # learn these label values from the downstream
        dynamic_labels:
          - __name__
          - job
        # how often to re-sync them
        sync_interval: 5m
        # always in the filter, without polling
        static_labels_include:
          instance:
            - instance1
        # removed from the filter, applied last
        static_labels_exclude:
          __name__:
            - up
        # behavior before the first successful sync
        on_sync_error: abort
```

| Option | Default | Meaning |
| ------ | ------- | ------- |
| `dynamic_labels` | — | Labels whose values promxy queries from the downstream. |
| `sync_interval` | — | Re-sync period for dynamic labels. |
| `static_labels_include` | — | Label values always present in the filter, no polling needed. |
| `static_labels_exclude` | — | Label values removed from the filter. Applied **last**, so it overrides both dynamic and static includes. |
| `on_sync_error` | `abort` | Behavior while no sync has ever succeeded. |

`__name__` is usually the highest-value label to filter on: it is what most
queries constrain, and its cardinality is small relative to the series space.

## Startup behaviour: `on_sync_error`

A dynamic filter is useless until it has synced at least once, and a downstream
may be unreachable exactly when promxy starts. `on_sync_error` decides what
happens in that window:

| Value | Behaviour before the first successful sync |
| ----- | ------------------------------------------ |
| `abort` *(default)* | The sync fails, which propagates up and **blocks promxy startup** until a sync succeeds. Preserves historical behaviour. |
| `open` | Startup proceeds with no filtering — every query is sent downstream. Fail-open. |
| `closed` | Startup proceeds but the filter rejects everything — the target is skipped entirely. Fail-closed. |

`abort` is safest for correctness but means one unreachable downstream can stop
promxy from starting at all. If that trade-off is wrong for you, `open` keeps
promxy serving with the fan-out it would have had without `label_filter`
configured, and starts filtering once the first sync lands.

When no explicit `sync_interval` is set, promxy retries the initial sync every
5s until it succeeds.

This setting is about the *sync* path only. It is unrelated to the server
group's `ignore_error` / `downgrade_error`, which govern the *query* path.

## Observability

Three metrics track the filter — see [Metrics](../operations/metrics.md):

- `promxy_label_filter_sync_count_total{status}` — syncs completed, by outcome
- `promxy_label_filter_sync_duration_seconds{status}` — sync latency
- `promxy_label_filter_filtered_count_total{type}` — requests filtered out, by
  query type

If `promxy_label_filter_filtered_count_total` stays at zero, your filter isn't
buying anything and the config is adding risk for no benefit. If queries return
unexpectedly empty, check whether a stale or over-broad filter is dropping them
before looking at the downstreams.

## Related options

`label_filter` only *skips* downstreams; it never changes the query that is
sent. If you want to constrain what a group returns, see
[`inject_matchers`](../configuration/server-groups.md#inject_matchers), and if
you only need to know where a result came from, static
[`labels`](../configuration/server-groups.md#labels) are simpler.

Where the split is by *time* rather than by label — a long-term-storage group
holding only data older than 3h, or a decommissioned group — use
[`relative_time_range` / `absolute_time_range`](../configuration/server-groups.md#relative_time_range--absolute_time_range)
instead. Those are exact, need no syncing, and can't go stale.
