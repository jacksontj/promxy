# Rules and alerting

Promxy evaluates alerting rules against your **entire** infrastructure. That is
the main reason to run rules here rather than on individual Prometheus servers:
"the global error rate is above 10%" is impossible to express on a single shard
without federation or re-scraping, and trivial in promxy.

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

This is the one thing that genuinely differs, and it has two consequences.

**Recording rules require `remote_write`.** Prometheus writes recording-rule
output to its local TSDB. Promxy has no TSDB, so it needs somewhere to send
them. Configuring a recording rule with no `remote_write` endpoint is a **fatal
config error**.

**Alerting rules should have `remote_write` too.** They work without it — alerts
fire and reach Alertmanager — but the `ALERTS` and `ALERTS_FOR_STATE` series
have nowhere to go, so you cannot query alert state and promxy logs a warning at
startup.

```yaml
remote_write:
  - url: http://localhost:8083/receive
```

Anything promxy would "write" (as opposed to proxy) goes here.

### `max_samples_per_send`

Promxy defaults `queue_config.max_samples_per_send` to **100**, where upstream
Prometheus uses 2000. Recording-rule output over large or high-cardinality
series can decompress past the 32 MiB snappy limit that Prometheus 3.5.3+
enforces on the receiving side; the receiver then rejects the batch with
`snappy: decoded length N exceeds limit 33554432` and the queue manager drops
it. The lower default keeps requests comfortably under that limit.

Set it explicitly to override:

```yaml
remote_write:
  - url: http://localhost:8083/receive
    queue_config:
      max_samples_per_send: 500
```

### WAL durability

Samples are appended to a WAL that the remote_write queue managers tail. Pass
`--storage.path` to keep that WAL on disk across restarts; without it promxy
uses a temporary directory that is deleted on shutdown, so anything not yet
shipped is lost when promxy stops.

## Where to send `remote_write`

Any remote_write receiver works — a Prometheus with
`--web.enable-remote-write-receiver`, VictoriaMetrics, Mimir, Cortex, Thanos
Receive.

If you just want the metrics to be scrapeable again, promxy ships a small
companion binary, `remote_write_exporter`, that keeps the most recent value of
each received series in memory and re-exposes it on `/metrics`:

```
remote_write_exporter --metric-ttl=5m
```

It listens on `:8083` and accepts remote_write at `/receive`. Flags are in the
[CLI reference](../configuration/cli-flags.md#remote_write_exporter). It is a
convenience, not a storage system — everything lives in memory behind a TTL
sweep.

A common loop is to point `remote_write` at `remote_write_exporter` and have one
of your Prometheus servers scrape it, which puts recording-rule output back into
the infrastructure promxy is querying.

## Alert state across restarts

Prometheus restores the `for` state of pending alerts at startup by querying
`ALERTS_FOR_STATE`. Two flags tune that restoration:

- `--rules.alert.for-outage-tolerance` (default `1h`) — how long an outage may
  have been for restoration to still apply.
- `--rules.alert.for-grace-period` (default `10m`) — minimum delay between an
  alert firing and its restored `for` state; only applies to alerts whose `for`
  exceeds this.

If your `remote_write` target doesn't retain `ALERTS_FOR_STATE` — or you have no
`remote_write` at all — promxy can instead **recompute** alert state at startup:

```
promxy --config=config.yaml --rules.alertbackfill
```

With `--rules.alertbackfill`, when the alert-state query returns nothing promxy
re-runs the underlying alert expression over the relevant window against your
downstreams and reconstructs the series. This costs queries at startup, but it
means a promxy restart doesn't reset every pending alert's `for` timer.

## `GeneratorURL`

By default alerts carry a Prometheus-style `GeneratorURL` pointing at promxy's
own graph page, built from `--web.external-url`. Set that flag if promxy sits
behind a reverse proxy, or the link will point at promxy's hostname and port.

To send alerts to Grafana, PagerDuty, or a runbook instead, see
[Alert templates](../configuration/alert-templates.md).

## Alertmanager authentication

The `alerting.alertmanagers` block accepts the standard Prometheus HTTP client
config (`basic_auth`, `authorization`, `tls_config`, …), plus custom headers for
gateways that authenticate on a header:

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

`values`, `secrets`, and `files` are all supported for a header's value.

## Tuning

- `--alertmanager.notification-queue-capacity` (default `10000`) — pending
  notification queue depth.
- `--rules.alert.resend-delay` (default `1m`) — minimum interval before an alert
  is resent.
- `global.evaluation_interval` — how often rules run. Remember that every
  evaluation is a scatter-gather across all your server groups, so this is
  meaningfully more expensive than on a single Prometheus.
