# Architecture

Promxy is stateless: no TSDB, no scrape manager. It speaks the Prometheus HTTP
API to its clients, and the Prometheus HTTP API (optionally remote_read) to its
downstreams.

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

**Within a server group** — members hold the *same* data (HA replicas). Promxy
fans out to every target, requires **one** success, and merges with
anti-affinity dedup: overlapping samples collapse, holes in one replica are
filled from another. See [HA and merging](ha-and-merging.md).

**Across server groups** — groups hold *different* data (shards). Promxy fans
out to every group and unions the results, with an anti-affinity buffer of zero
(only exactly-coincident duplicates collapse) and **all** groups required to
succeed.

Two consequences:

- A single dead group fails the whole query. `ignore_error` /
  `downgrade_error` opt individual groups out.
- `count(up)` is only correct if each group's data is distinct —
  hence static `labels` per group, and one group per tenant on multi-tenant
  backends.

## Query pushdown (`NodeReplacer`)

Promxy pushes as much of each query as it safely can down to the Prometheus
servers rather than pulling raw series back and evaluating locally.

The engine is configured with a `NodeReplacer`
([`pkg/proxystorage/proxy.go`](../../pkg/proxystorage/proxy.go)). Before
evaluation it walks the AST and asks promxy whether each node can be replaced;
promxy answers by executing that subtree remotely and substituting a synthetic
`VectorSelector` holding the result.

Rules, in priority order:

1. **Correctness beats speed.** If a rewrite could change the answer, promxy
   declines and the engine evaluates locally.
2. **No nested aggregations.** An `AggregateExpr` child has its own combining
   logic, so the subtree isn't safe as-is.
3. **Offsets in the subtree must agree.** Promxy waits until it is far enough
   down the tree that they converge.
4. **No loss of granularity.**

### What gets pushed down

**Aggregations**, when the operation is *reentrant* (applying it to partial
results and again to those results gives the same answer):

| Operation | Handling |
| --------- | -------- |
| `sum`, `min`, `max`, `topk`, `bottomk`, `group` | Pushed down directly; re-applied locally over the per-downstream results. |
| `count` | Pushed down as `count`, combined locally with `sum`. |
| `count_values` | Pushed down, combined as `sum(count_values(...)) by (key)`. |
| `avg` | Rewritten as `sum(...) / count(...)`, both of which push down. |
| `quantile`, `stddev`, `stdvar` | Not pushed down; need the full data set. |
| `limitk`, `limit_ratio` | Not pushed down. The engine selects series by hash over the *complete* input vector, so per-downstream selection would pick an inconsistent subset. |

**Function calls** (`rate`, `increase`, `*_over_time`, …) are pushed down
wholesale. Exceptions:

- `absent`, `absent_over_time` — hard to reconstruct at this layer; promxy
  pushes down elsewhere in the tree instead.
- `label_join`, `label_replace`, `info` — the engine evaluates these through
  dedicated dispatchers with precise error messages, which promxy's error
  wrappers would mangle.

**Selectors and subqueries** are pushed down where the offset/`@` rules allow.
With `@` in play the downstream resolves `@ T offset O` itself, so promxy must
not strip offsets or shift the request window. For step-invariant `@` subtrees
promxy issues one instant query and replicates the result across the step grid.

Anything not pushed down falls through to `ProxyStorage.Querier` →
`proxyquerier` → the same fan-out, fetching raw data instead.

## The per-target client stack

Most per-group options are decorators around a base API client, one stack per
discovered target. Innermost first:

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

`inject_matchers` sits *beneath* the label-manipulation layers deliberately, so
its matchers reach the downstream verbatim without interacting with
`label_filter`'s query filtering or `metrics_relabel`'s matcher reversal.

Those stacks combine into the group client (anti-affinity merge, one success
required), wrapped with `servergroup ord=N` error annotation, then with
`ignore_error` / `downgrade_error`. Group clients combine into the cross-group
client, wrapped in a time-truncation layer.

## Config reload

Reload rebuilds the whole client stack: new groups are constructed, discovery
starts, and the new state waits for every group to be ready before being swapped
in atomically. Only then is the old state cancelled. If any group fails to
apply, the new state is discarded and the old one keeps serving.

## Writes

Promxy's Appender is `remote_write`. Recording rules and alert-state series are
appended to a WAL-only agent-mode DB; the remote_write queue managers tail that
WAL and ship it. `--storage.path` makes the WAL durable across restarts;
without it it lives in a temp directory removed on shutdown. With no
`remote_write` configured the appender discards everything.
