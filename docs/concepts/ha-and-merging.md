# HA and merging

Prometheus has no clustering or HA story of its own, so the accepted practice is
to run N servers with an identical config, each scraping everything
independently. That gives you redundancy but leaves you with N datasources that
nobody can aggregate across.

Promxy's job is to turn those N servers back into one. The hard part is that
their data is *nearly* — but not exactly — identical.

## Why the data doesn't line up

Prometheus stores the timestamp at which a scrape **starts**. Two servers
scraping the same target will start their scrapes at different moments, and the
same server's scrape duration varies with exporter latency, serialisation, and
network jitter. Add clock drift between hosts and you get two series that
represent the same measurements at slightly different timestamps.

Naively unioning them doubles your sample count. Naively taking one and ignoring
the other loses the gap-filling that made HA worth running.

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

The merge works like this
([`pkg/promhttputil/merge.go`](../../pkg/promhttputil/merge.go)):

1. Of the two series being merged, the one with **more** samples becomes the
   base. When one downstream has a hole and the other doesn't, this picks the
   complete side rather than merging up from the worse one.
2. Samples from the other side that fall **before** the base's first sample
   (minus the buffer) are prepended.
3. Walking the base, whenever the gap between two consecutive samples exceeds
   `2 × anti_affinity`, promxy looks for a sample from the other side that fits
   in the middle of that gap without landing within `anti_affinity` of either
   neighbour.

So a buffer of `10s` tolerates about 5s of skew on either side. Duplicate
samples collapse; genuine holes get filled.

**Set `anti_affinity` to your scrape interval.** That is the value at which a
missing sample is a real gap and anything closer is skew.

### Getting it wrong

- **Too small** — skewed duplicates from the second replica survive the merge.
  A `count_over_time` roughly doubles; rates and deltas get noisy.
- **Too large** — legitimately-spaced samples are treated as duplicates and
  dropped, thinning your series.

## `anti_affinity_dynamic`

A single static buffer only works when every series in the group shares a scrape
interval. If one group serves jobs scraped at 15s alongside jobs scraped at 1m,
no single value is right: `15s` is too wide for the fast job and far too tight
for the slow one, whose normal 60s spacing looks like a gap and gets filled.

```yaml
anti_affinity_dynamic: true
```

With this set, promxy infers the buffer **per series** from the data itself —
half the median inter-sample gap of the longer side, which models "scrape
interval / 2" without you having to declare it. `anti_affinity` remains the
fallback for series with too few samples (fewer than three gaps) to estimate
from.

See [issue #734](https://github.com/jacksontj/promxy/issues/734).

## `prefer_max`

When both sides have a sample at the same timestamp, promxy has to pick one. By
default it takes the base series' value; `prefer_max: true` takes the larger.
This is occasionally useful for monotonic counters where one replica missed
increments, but it is not a substitute for correct `anti_affinity`.

## Native histograms

Float samples and native-histogram samples coexist on a series and are merged
independently, so a series carrying both still deduplicates correctly. Note that
histogram fidelity through the *JSON* API is lossy — see
[Native histograms](../guides/native-histograms.md) for why `remote_read`
matters here.

## Availability vs. correctness

Within a group, promxy requires **one** target to succeed. Across groups it
requires **all** of them, because a missing shard means silently missing data
and promxy would rather error than hand alerting rules a wrong answer.

To trade that for availability, per group:

- `downgrade_error: true` — errors become warnings on the response. Preferred:
  you keep serving, and clients that surface warnings still see the problem.
- `ignore_error: true` — errors are dropped entirely. The query succeeds with
  no indication that a shard was missing.

Neither should be the default for groups whose data alerts depend on.

## Deduplicating replica labels

If your Prometheus servers add a distinguishing external label (`replica`,
`prometheus_replica`, …), their series won't have identical label sets and
promxy can't merge them. Drop the label in the query path:

```yaml
metrics_relabel_configs:
  - action: labeldrop
    source_label: replica
```

This is the same idea as Thanos' replica-label deduplication. Because promxy
rewrites incoming query matchers to match, the label must be dropped with a
*reversible* action — see
[`metrics_relabel_configs`](../configuration/server-groups.md#metrics_relabel_configs).
