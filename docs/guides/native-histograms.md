# Native histograms

Prometheus' JSON HTTP API cannot represent a native histogram without loss.
Promxy's remote_read path can. That single fact drives all of the configuration
below.

Promxy's default posture is **fail loud**: if it can tell a query involves
native histograms and the target server group has no `remote_read` configured,
the query errors out before fan-out rather than quietly returning degraded data.
Wrong data is worse than no data — especially when alerting rules consume it.

## The fix

Enable `remote_read` on every server group whose data includes native
histograms:

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prom-01:9090, prom-02:9090]
      remote_read: true
```

That is the whole fix for most deployments. Note that some
Prometheus-compatible backends don't implement remote_read at all (notably
VictoriaMetrics); see [Escape hatch](#escape-hatch) below for those.

## How promxy detects a histogram query

### AST detection (always on)

Promxy walks the query AST looking for calls to the PromQL functions that only
operate on native histograms:

`histogram_avg`, `histogram_count`, `histogram_sum`, `histogram_stddev`,
`histogram_stdvar`, `histogram_fraction`

A query containing any of these is unambiguously histogram-bearing, no metadata
required.

`histogram_quantile` is deliberately **not** in that set: it accepts both
classic `_bucket` float series and native histograms, so on its own it says
nothing about which you have.

This costs nothing and needs no configuration, but it misses queries that touch
a histogram metric without calling one of those functions — `rate(my_hist[5m])`
being the obvious example.

### Metadata detection (opt-in)

To catch those, let promxy learn which metric *names* are histogram-typed:

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prom-01:9090]
      remote_read: true
      native_histogram:
        metadata_refresh: 5m
```

Promxy periodically calls `/api/v1/metadata` on the group, extracts the
histogram-typed metric names, and consults the union of all groups' caches when
classifying a query. The cache is keyed by metric *name*, not series, so its
memory footprint scales with your histogram-name count (typically a low
single-digit percentage of the name space) rather than cardinality.

Set this whenever you have native histograms and want the safety net to be
complete.

## Escape hatch

If a backend can't do remote_read and you accept the fidelity loss — say
dashboards that only ever consume `histogram_quantile` output and don't care
about sparse spans — opt in explicitly per group:

```yaml
native_histogram:
  allow_lossy: true
```

The query then proceeds over the JSON API. This is a deliberate, per-group
decision; there is no global switch, and the default stays `false`.

## Behaviour summary

| `remote_read` | `allow_lossy` | Histogram-bearing query |
| ------------- | ------------- | ----------------------- |
| `true` | *(any)* | Served losslessly via remote_read. |
| `false` | `false` *(default)* | Errors before fan-out, naming the groups missing `remote_read`. |
| `false` | `true` | Served via the JSON API; histogram samples are degraded. |

Promxy also detects lossy histogram data arriving from a pushed-down query at
runtime: if a downstream's response to a pushed-down aggregation contains a
degraded histogram, promxy abandons the pushdown for that node and re-fetches
raw data through the normal querier path, which routes via remote_read where
configured.

## Merging

Float and histogram samples coexist on a series and are merged independently, so
a series carrying both still deduplicates correctly through the anti-affinity
merge. Histograms round-trip losslessly through that merge. See
[HA and merging](../concepts/ha-and-merging.md).
