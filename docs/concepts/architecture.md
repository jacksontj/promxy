# Architecture

Promxy is a stateless aggregating proxy. It has no TSDB, no scrape manager, and
no storage of its own — it speaks the Prometheus HTTP API to its clients and the
Prometheus HTTP API (and optionally remote_read) to its downstreams.

```
      Grafana / curl / Alertmanager
                  |
                  v
     +---------------------------+
     |  HTTP server (:8082)      |
     |  prometheus web UI + API  |
     +------------+--------------+
                  |
                  v
     +---------------------------+
     |  PromQL engine            |
     |  + NodeReplacer (pushdown)|
     +------------+--------------+
                  |
                  v
     +---------------------------+
     |  ProxyStorage             |   union across groups (all required)
     +------+-------------+------+
            |             |
            v             v
    +---------------+ +---------------+
    | server group A| | server group B|   HA merge within group
    +---+-------+---+ +---+-------+---+   (anti-affinity dedup)
        |       |         |       |
       prom1  prom2     prom3   prom4
```

## The two layers of merging

This distinction explains most of promxy's behaviour, so it is worth stating
precisely.

**Within a server group** — the members hold the *same* data (HA replicas).
Promxy fans a request out to every target, requires **one** success, and merges
the successful responses with anti-affinity dedup: overlapping samples collapse,
and a hole in one replica is filled from another. See
[HA and merging](ha-and-merging.md).

**Across server groups** — the groups hold *different* data (shards). Promxy
fans out to every group and unions the results, with an anti-affinity buffer of
zero (only exactly-coincident duplicates collapse) and **all** groups required
to succeed. That "all required" default is why a single dead group fails the
whole query — and why `ignore_error` / `downgrade_error` exist to opt individual
groups out of it.

Everything else follows from this: label-less aggregations like `count(up)` are
only correct if each group's data is genuinely distinct, which is why static
`labels` per group matter, and why multi-tenant backends need one group per
tenant.

## Query pushdown (`NodeReplacer`)

The naive implementation would fetch all raw series from every downstream and
evaluate locally. Promxy instead pushes as much of the query as it safely can
down to the Prometheus servers, which are far better placed to run it — they
have the data locally.

Promxy's PromQL engine is configured with a `NodeReplacer`
([`pkg/proxystorage/proxy.go`](../../pkg/proxystorage/proxy.go)). Before
evaluation, the engine walks the AST and asks promxy whether each node can be
replaced. Promxy answers by *executing that subtree remotely* and substituting a
synthetic `VectorSelector` holding the result.

The ground rules, in priority order:

1. **Correctness beats speed.** If a rewrite could change the answer, promxy
   declines and lets the engine evaluate locally over raw data.
2. **No nested aggregations.** A child that is itself an `AggregateExpr` has its
   own combining logic, so the subtree isn't safe to push down as-is.
3. **Offsets in the subtree must agree.** Mismatched offsets would fetch
   mismatched data; promxy waits until it is far enough down the tree that they
   converge.
4. **No loss of granularity.** Pushdown must not reduce accuracy.

### What gets pushed down

**Aggregations** are pushed down when the operation is *reentrant* — applying it
to partial results and then again to those results gives the same answer:

| Operation | Handling |
| --------- | -------- |
| `sum`, `min`, `max`, `topk`, `bottomk`, `group` | Pushed down directly; re-applied locally over the per-downstream results. |
| `count` | Pushed down as `count`, then combined locally with `sum`. |
| `count_values` | Pushed down, then combined as `sum(count_values(...)) by (key)`. |
| `avg` | Rewritten as `sum(...) / count(...)`, both of which push down. |
| `quantile` | Not pushed down — a true quantile needs the full data set. |
| `stddev`, `stdvar` | Not pushed down — need the full data set. |
| `limitk`, `limit_ratio` | Not pushed down — the engine selects series by hash over the *complete* input vector, so per-downstream selection would pick an inconsistent subset. |

**Function calls** (`rate`, `increase`, `*_over_time`, …) are pushed down
wholesale — the downstream computes them over its local raw data. Exceptions:

- `absent`, `absent_over_time` — semantics are hard to reconstruct at this
  layer, so promxy pushes down elsewhere in the tree instead.
- `label_join`, `label_replace`, `info` — the engine evaluates these through
  dedicated dispatchers with precise error messages; pushing them to a single
  downstream would mangle that wording as the error round-trips through promxy's
  error wrappers.

**Selectors and subqueries** are pushed down where the offset/`@` rules allow.
Queries using the `@` modifier are handled specially: the downstream resolves
`@ T offset O` internally, so promxy must not strip offsets or shift the request
window. For step-invariant `@` subtrees promxy issues a single instant query and
replicates the result across the step grid.

Anything not pushed down falls through to `ProxyStorage.Querier` →
`proxyquerier` → the same server-group fan-out, fetching raw data instead.

## The per-target client stack

Most per-server-group options are implemented as decorators around a base API
client, one stack per discovered target. Innermost first:

| Layer | Configured by |
| ----- | ------------- |
| HTTP API client | `scheme`, `path_prefix`, `query_params`, `http_headers`, `http_client`, `timeout` |
| Debug logging | `--log-level=debug` |
| remote_read | `remote_read`, `remote_read_path` |
| Absolute / relative time filter | `absolute_time_range`, `relative_time_range` |
| Step re-alignment | `align_query_range_with_step` |
| Matcher injection | `inject_matchers` |
| Label addition | `labels` + labels from `relabel_configs` |
| Metric relabeling | `metrics_relabel_configs` |
| Label filtering | `label_filter` |
| Error wrapping | *(always — annotates errors with `target=…`)* |

The stack order is deliberate: `inject_matchers` sits *beneath* the
label-manipulation layers so its matchers reach the downstream verbatim, without
interacting with `label_filter`'s query filtering or `metrics_relabel`'s
matcher reversal.

Those per-target stacks are then combined into the group-level client
(anti-affinity merge, one success required), wrapped with `servergroup ord=N`
error annotation, and finally with `ignore_error` / `downgrade_error` if
configured. The group clients are combined into the cross-group client, and the
whole thing is wrapped in a time-truncation layer.

## Config reload

Reload rebuilds the entire client stack: new server groups are constructed,
discovery is started, and the new state waits until every group is ready before
being atomically swapped in. Only then is the old state cancelled. If any group
fails to apply, the new state is discarded and the old one keeps serving — a bad
reload never takes promxy down.

## Writes

Promxy's Appender is `remote_write`. Recording rules and alert-state series are
appended to a WAL-only agent-mode DB, whose WAL the remote_write queue managers
tail and ship to the configured endpoints. With `--storage.path` that WAL is
durable across restarts; without it, it lives in a temp directory removed on
shutdown. With no `remote_write` configured the appender is a stub that discards
everything.
