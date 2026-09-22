# HA and merging

Promxy merges the N Prometheus servers in a server group back into one series
set. Their data is nearly, but not exactly, identical.

## Why the data doesn't line up

Prometheus stores the timestamp at which a scrape **starts**. Two servers start
their scrapes at different moments, and scrape duration varies with exporter
latency and network jitter. Add clock drift and you have two series recording
the same measurements at slightly different timestamps.

Union them and your sample count doubles. Take one and ignore the other and you
lose the gap-filling that HA was for.

## `anti_affinity`

Promxy merges with an **anti-affinity buffer**: it refuses to place a sample
within `anti_affinity` of an existing one.

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prom-01:9090, prom-02:9090]
      anti_affinity: 10s   # default
```

The algorithm ([`pkg/promhttputil/merge.go`](../../pkg/promhttputil/merge.go)):

1. The series with **more** samples becomes the base, so a side with a hole
   never wins over a complete one.
2. Samples from the other side falling before the base's first sample (minus
   the buffer) are prepended.
3. Walking the base, wherever the gap between consecutive samples exceeds
   `2 × anti_affinity`, promxy looks for a sample from the other side that fits
   in that gap without landing within `anti_affinity` of either neighbour.

A `10s` buffer tolerates ~5s of skew on either side.

**Set `anti_affinity` to your scrape interval.**

| Setting | Symptom |
| ------- | ------- |
| Too small | Skewed duplicates survive. `count_over_time` roughly doubles; rates get noisy. |
| Too large | Legitimately-spaced samples are dropped as duplicates, thinning the series. |

## `anti_affinity_dynamic`

A static buffer only works when every series in the group shares a scrape
interval. With 15s and 1m jobs in one group, no single value fits: `15s` is too
tight for the 1m job (its normal spacing looks like a gap and gets filled) and
too wide for the 15s one.

```yaml
anti_affinity_dynamic: true
```

Promxy then infers the buffer **per series** — half the median inter-sample gap
of the longer side. `anti_affinity` remains the fallback for series with fewer
than three gaps to estimate from. See
[issue #734](https://github.com/jacksontj/promxy/issues/734).

## `prefer_max`

When both sides have a sample at the same timestamp, promxy takes the base
series' value. `prefer_max: true` takes the larger instead. Not a substitute
for correct `anti_affinity`.

## Native histograms

Float and histogram samples on a series are merged independently, so a series
carrying both deduplicates correctly, and histograms round-trip losslessly.
Histogram fidelity through the *JSON* API is lossy, though — see
[Native histograms](../guides/native-histograms.md).

## Availability vs. correctness

Within a group promxy requires **one** target to succeed. Across groups it
requires **all** of them: a missing shard means silently missing data, and an
error beats a wrong answer that alerting depends on.

To trade that for availability, per group:

| Option | Effect |
| ------ | ------ |
| `downgrade_error: true` | Errors become warnings on the response. Preferred — clients that surface warnings still see the problem. |
| `ignore_error: true` | Errors dropped entirely. No indication a shard was missing. |

## Deduplicating replica labels

If your Prometheus servers add a distinguishing external label (`replica`,
`prometheus_replica`), their series have different label sets and can't be
merged. Drop it in the query path:

```yaml
metrics_relabel_configs:
  - action: labeldrop
    source_label: replica
```

Same idea as Thanos' replica-label dedup. The action must be *reversible*
because promxy rewrites incoming query matchers to match — see
[`metrics_relabel_configs`](../configuration/server-groups.md#metrics_relabel_configs).
