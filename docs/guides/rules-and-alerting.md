# Rules and alerting

Promxy evaluates alerting rules across your entire infrastructure, so global
rules ("error rate above 10% everywhere") that no single shard can express
become trivial.

Rules are configured exactly as in Prometheus:

```yaml
global:
  evaluation_interval: 30s

rule_files:
  - "rules/*.yml"

alerting:
  alertmanagers:
    - scheme: http
      static_configs:
        - targets: [alertmanager:9093]
```

## Promxy has no local storage

**Recording rules require `remote_write`.** Prometheus writes recording-rule
output to its local TSDB; promxy has none. A recording rule with no
`remote_write` endpoint is a **fatal config error**.

**Alerting rules should have it too.** They work without it — alerts fire and
reach Alertmanager — but `ALERTS` and `ALERTS_FOR_STATE` have nowhere to go, so
alert state isn't queryable. Promxy logs a warning at startup.

```yaml
remote_write:
  - url: http://localhost:8083/receive
```

### `max_samples_per_send`

Promxy defaults this to **100**, where upstream Prometheus uses 2000.
Recording-rule output over high-cardinality series can decompress past the
32 MiB snappy limit Prometheus 3.5.3+ enforces on the receiver, which then
rejects the batch with `snappy: decoded length N exceeds limit 33554432` and the
queue manager drops it.

Override explicitly:

```yaml
remote_write:
  - url: http://localhost:8083/receive
    queue_config:
      max_samples_per_send: 500
```

### WAL durability

Samples go to a WAL that the queue managers tail. `--storage.path` keeps it on
disk across restarts; without it promxy uses a temp directory deleted on
shutdown, losing anything not yet shipped.

## Where to send `remote_write`

Any receiver works — Prometheus with `--web.enable-remote-write-receiver`,
VictoriaMetrics, Mimir, Cortex, Thanos Receive.

To just make the metrics scrapeable again, promxy ships `remote_write_exporter`,
which keeps the most recent value of each received series in memory and exposes
it on `/metrics`:

```
remote_write_exporter --metric-ttl=5m
```

It listens on `:8083`, accepts remote_write at `/receive`, and holds everything
in memory behind a TTL sweep — a convenience, not storage. Flags:
[CLI reference](../configuration/cli-flags.md#remote_write_exporter).

A common loop is to point `remote_write` at it and have one of your Prometheus
servers scrape it, putting recording-rule output back into the infrastructure
promxy queries.

## Alert state across restarts

Prometheus restores the `for` state of pending alerts by querying
`ALERTS_FOR_STATE`:

- `--rules.alert.for-outage-tolerance` (default `1h`) — how long an outage may
  have been for restoration to still apply.
- `--rules.alert.for-grace-period` (default `10m`) — minimum delay between an
  alert firing and its restored `for` state; only for alerts whose `for` exceeds
  this.

If your `remote_write` target doesn't retain `ALERTS_FOR_STATE` — or you have
none — promxy can recompute alert state at startup:

```
promxy --config=config.yaml --rules.alertbackfill
```

When the alert-state query returns nothing, promxy re-runs the alert expression
over the relevant window against your downstreams and reconstructs the series.
Costs queries at startup; keeps restarts from resetting every `for` timer.

## `GeneratorURL`

Alerts carry a Prometheus-style `GeneratorURL` pointing at promxy's graph page,
built from `--web.external-url`. Set that flag if promxy sits behind a reverse
proxy, or links point at promxy's own hostname and port.

For Grafana, PagerDuty, or runbook links instead, see
[Alert templates](../configuration/alert-templates.md).

## Alertmanager authentication

`alerting.alertmanagers` accepts the standard Prometheus HTTP client config
(`basic_auth`, `authorization`, `tls_config`, …), plus custom headers for
gateways that authenticate on one:

```yaml
alerting:
  alertmanagers:
    - scheme: https
      static_configs:
        - targets: [alertmanager.example.com]
      http_headers:
        X-Gateway-Signature:
          values: ["my-secret-token"]
```

`values`, `secrets`, and `files` are all supported.

## Tuning

- `--alertmanager.notification-queue-capacity` (default `10000`) — pending
  notification queue depth.
- `--rules.alert.resend-delay` (default `1m`) — minimum interval before resend.
- `global.evaluation_interval` — every evaluation is a scatter-gather across all
  server groups, so this costs more than on a single Prometheus.
