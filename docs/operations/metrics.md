# Metrics

Promxy exposes metrics on `/metrics` (path set by `--metrics-path`): its own,
those of the Prometheus libraries it embeds, and the usual Go/process ones.

Label-bearing metrics only appear **after their first observation**.
`server_group_request_duration_seconds` is absent until promxy has made a
downstream request; `promxy_label_filter_*` until a `label_filter` has run.

## Promxy's own

### `server_group_targets`

Gauge. Number of targets currently discovered for a server group.

Labels: `ordinal` (the group's index in the config, always unique) and `name`
(the optional `name:`, empty when unset).

```
server_group_targets{name="g1",ordinal="0"} 2
```

The most valuable promxy alert. A group with zero targets errors every query,
or with `ignore_error` silently contributes nothing:

```yaml
- alert: PromxyServerGroupEmpty
  expr: server_group_targets == 0
  for: 5m
  annotations:
    summary: "promxy server group {{ $labels.ordinal }} ({{ $labels.name }}) has no targets"
```

Promxy also logs a warning when a group transitions to zero targets, and at
startup if one begins with none.

### `server_group_request_duration_seconds`

Summary. Latency of calls promxy makes to individual downstream targets.

Labels:

- `host` — the downstream target
- `call` — `query`, `query_range`, `get_value`, `series`, `label_names`,
  `label_values`, `query_exemplars`
- `status` — `success` or `error`

```
server_group_request_duration_seconds_count{call="query",host="prom-01:9090",status="error"} 1
```

Downstream error rate:

```promql
sum(rate(server_group_request_duration_seconds_count{status="error"}[5m])) by (host)
  /
sum(rate(server_group_request_duration_seconds_count[5m])) by (host)
```

`get_value` is the raw-data fetch used when a query couldn't be pushed down. A
high `get_value` share relative to `query`/`query_range` means promxy is
evaluating locally — see
[Architecture](../concepts/architecture.md#query-pushdown-nodereplacer).

### `promxy_label_filter_sync_count_total`

Counter, labelled by `status`. Syncs completed by a `label_filter`.

### `promxy_label_filter_sync_duration_seconds`

Summary, labelled by `status`. Sync latency.

### `promxy_label_filter_filtered_count_total`

Counter, labelled by `type` (query type). Requests the filter kept from going
downstream. Zero means the filter is buying nothing. See
[Label filtering](../guides/label-filtering.md).

### Config reload

- `prometheus_config_last_reload_successful` — `1` or `0`
- `prometheus_config_last_reload_success_timestamp_seconds`
- `process_reload_time_seconds` — timestamp of the last `SIGHUP`

```yaml
- alert: PromxyConfigReloadFailed
  expr: prometheus_config_last_reload_successful == 0
  for: 5m
```

### `promxy_build_info`

Constant `1`, labelled with `version`, `revision`, `branch`, `goversion`,
`goos`, `goarch`.

## Inherited from the Prometheus libraries

Promxy embeds Prometheus' query engine, rule manager, notifier, service
discovery, and remote_write; each registers its usual metrics, which behave as
documented upstream.

| Family | Covers |
| ------ | ------ |
| `prometheus_engine_*` | Query duration, samples, concurrency (`prometheus_engine_query_duration_seconds`, `prometheus_engine_query_samples_total`, `prometheus_engine_queries`, `prometheus_engine_queries_concurrent_max`) |
| `prometheus_rule_*` | Rule evaluation (`prometheus_rule_evaluation_duration_seconds`, `prometheus_rule_evaluation_failures_total`, `prometheus_rule_group_iterations_missed_total`) |
| `prometheus_notifications_*` | Alertmanager delivery (`prometheus_notifications_dropped_total`, `prometheus_notifications_queue_length`, `prometheus_notifications_queue_capacity`, `prometheus_notifications_alertmanagers_discovered`) |
| `prometheus_remote_storage_*` | `remote_write` shipping (`prometheus_remote_storage_samples_pending`, `prometheus_remote_storage_samples_failed_total`, `prometheus_remote_storage_shards`, `prometheus_remote_storage_sent_batch_duration_seconds`) |
| `prometheus_agent_*`, `prometheus_wal_*` | The WAL backing `remote_write` |
| `prometheus_sd_*` | Service discovery (`prometheus_sd_discovered_targets`, `prometheus_sd_failed_configs`, per-mechanism failure counters) |
| `prometheus_http_*`, `prometheus_api_*` | Promxy's own HTTP surface |

Useful alerts from these:

```yaml
- alert: PromxyAlertNotificationsDropped
  expr: rate(prometheus_notifications_dropped_total[5m]) > 0

- alert: PromxyRuleEvaluationFailures
  expr: rate(prometheus_rule_evaluation_failures_total[5m]) > 0

- alert: PromxyRemoteWriteFallingBehind
  expr: prometheus_remote_storage_samples_pending > 0 and rate(prometheus_remote_storage_samples_failed_total[5m]) > 0

- alert: PromxyMissedRuleEvaluations
  expr: rate(prometheus_rule_group_iterations_missed_total[5m]) > 0
```

## Go and process metrics

The standard `go_*`, `process_*`, `promhttp_*` collectors. Watch
`process_open_fds` against `process_max_fds`: each group holds up to
`max_idle_conns` (default 20000) idle connections.

## Dashboard

[`grafana.dashboard`](../../grafana.dashboard) at the repo root. Marked WIP —
a starting point, not a finished product.
