# Label filtering

Promxy scatter-gathers every query to every server group. With many shards,
most of those requests can only ever return nothing.

`label_filter` maintains an in-memory picture of which label values a downstream
has, and skips groups whose filter proves the query can't match.

> **Not a security mechanism.** It works from the query's matchers, so a caller
> can sidestep it by matching on a different label. See [Security](security.md).

## Configuration

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prom-us-01:9090]
      label_filter:
        dynamic_labels:
          - __name__
          - job
        sync_interval: 5m
        static_labels_include:
          instance:
            - instance1
        static_labels_exclude:
          __name__:
            - up
        on_sync_error: abort
```

| Option | Default | Meaning |
| ------ | ------- | ------- |
| `dynamic_labels` | — | Labels whose values promxy queries from the downstream. |
| `sync_interval` | — | Re-sync period for dynamic labels. |
| `static_labels_include` | — | Label values always in the filter, no polling. |
| `static_labels_exclude` | — | Label values removed from the filter. Applied **last**, overriding both dynamic and static includes. |
| `on_sync_error` | `abort` | Behavior while no sync has ever succeeded. |

`__name__` is usually the best label to filter on: most queries constrain it,
and its cardinality is small relative to the series space.

## Startup behaviour: `on_sync_error`

A dynamic filter is useless until it has synced once, and a downstream may be
unreachable exactly when promxy starts.

| Value | Behaviour before the first successful sync |
| ----- | ------------------------------------------ |
| `abort` *(default)* | Sync fails and **blocks promxy startup** until one succeeds. Historical behaviour. |
| `open` | Startup proceeds unfiltered; all queries go downstream. |
| `closed` | Startup proceeds but the filter rejects everything; the target is skipped. |

`abort` is safest but lets one unreachable downstream stop promxy from starting.
`open` keeps promxy serving with the fan-out it would have had without
`label_filter`, and starts filtering once the first sync lands.

With no `sync_interval` set, promxy retries the initial sync every 5s.

This governs the *sync* path only, not the server group's `ignore_error` /
`downgrade_error`, which govern the *query* path.

## Observability

See [Metrics](../operations/metrics.md):

- `promxy_label_filter_sync_count_total{status}` — syncs completed
- `promxy_label_filter_sync_duration_seconds{status}` — sync latency
- `promxy_label_filter_filtered_count_total{type}` — requests filtered out

If `promxy_label_filter_filtered_count_total` stays at zero, the filter isn't
buying anything. If queries return unexpectedly empty, check for a stale filter
before suspecting the downstreams.

## Related options

`label_filter` only skips downstreams; it never changes the query sent. To
constrain what a group returns, use
[`inject_matchers`](../configuration/server-groups.md#inject_matchers); to just
label where a result came from, use
[`labels`](../configuration/server-groups.md#labels).

For splits by *time* rather than label — long-term storage, a decommissioned
group — use
[`relative_time_range` / `absolute_time_range`](../configuration/server-groups.md#relative_time_range--absolute_time_range),
which are exact and can't go stale.
