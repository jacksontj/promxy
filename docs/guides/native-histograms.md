# Native histograms

Prometheus' JSON HTTP API cannot represent a native histogram without loss;
remote_read can.

By default promxy **fails loud**: if it detects a histogram-bearing query
against a group with no `remote_read`, the query errors before fan-out rather
than returning degraded data.

## The fix

Enable `remote_read` on every group whose data includes native histograms:

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prom-01:9090, prom-02:9090]
      remote_read: true
```

Some Prometheus-compatible backends don't implement remote_read (notably
VictoriaMetrics) — see [Escape hatch](#escape-hatch).

## How promxy detects a histogram query

### AST detection (always on)

Promxy looks for calls to the PromQL functions that only operate on native
histograms:

`histogram_avg`, `histogram_count`, `histogram_sum`, `histogram_stddev`,
`histogram_stdvar`, `histogram_fraction`

`histogram_quantile` is deliberately excluded: it accepts both classic
`_bucket` float series and native histograms, so it signals nothing on its own.

AST detection is free and needs no config, but misses queries that touch a
histogram metric without calling one of those functions, e.g.
`rate(my_hist[5m])`.

### Metadata detection (opt-in)

```yaml
native_histogram:
  metadata_refresh: 5m
```

Promxy periodically calls `/api/v1/metadata` on the group, extracts the
histogram-typed metric names, and consults the union of all groups' caches when
classifying a query. The cache is keyed by metric *name*, so memory scales with
histogram-name count rather than cardinality.

Set this if you have native histograms and want complete detection.

## Escape hatch

If a backend can't do remote_read and you accept the fidelity loss (e.g.
dashboards that only consume `histogram_quantile` output), opt in per group:

```yaml
native_histogram:
  allow_lossy: true
```

There is no global switch; the default stays `false`.

## Behaviour summary

| `remote_read` | `allow_lossy` | Histogram-bearing query |
| ------------- | ------------- | ----------------------- |
| `true` | *(any)* | Served losslessly via remote_read. |
| `false` | `false` *(default)* | Errors before fan-out, naming the groups missing `remote_read`. |
| `false` | `true` | Served via the JSON API; histogram samples degraded. |

Promxy also catches this at runtime: if a pushed-down query's response contains
a degraded histogram, promxy abandons the pushdown for that node and re-fetches
raw data through the querier path, which routes via remote_read where
configured.

## Merging

Float and histogram samples merge independently, so a series carrying both
deduplicates correctly and histograms round-trip losslessly. See
[HA and merging](../concepts/ha-and-merging.md).
