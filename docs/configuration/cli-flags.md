# Command-line flags

Everything below is from [`cmd/promxy/main.go`](../../cmd/promxy/main.go).
`promxy --help` prints the same list.

## General

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--version`, `-v` | | Print version and exit. |
| `--check-config` | | Load and validate the config file, then exit. |
| `--config` | `config.yaml` | Path to the config file. |

## Logging

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--log-level` | `info` | `panic`, `fatal`, `error`, `warn`, `info`, `debug`, `trace`. |
| `--log-format` | `text` | `text` or `json`. |
| `--log-max-form-prefix` | `256` | Max length of form values recorded in log entries. |
| `--access-log-destination` | `stdout` | `stdout`, `stderr`, or `none`. |

The embedded Prometheus libraries (notifier, discovery, web, rules) log through
the same logger, so these settings govern all output. Kubernetes client logging
is clamped below the verbosity at which it would print bearer tokens in clear
text.

## Web server

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--bind-addr` | `:8082` | Address to listen on. |
| `--web.config.file` | | *[Experimental]* Prometheus-format web config file: TLS, HTTP headers, basic auth users. See [Security](../guides/security.md). |
| `--web.cors.origin` | `.*` | Fully-anchored regex for allowed CORS origins. |
| `--web.read-timeout` | `5m` | Max duration for reading a request; also closes idle connections. |
| `--web.external-url` | | URL under which promxy is externally reachable. Used for links back to promxy (including alert `GeneratorURL`s). A path component prefixes all HTTP endpoints. |
| `--web.route-prefix` | path of `--web.external-url` | Prefix for internal routes. |
| `--web.enable-lifecycle` | `false` | Enable `POST /-/reload`. (`/-/quit` is also routed by the embedded Prometheus web handler, but promxy does not act on it — use `SIGTERM` to stop promxy.) |
| `--metrics-path` | `/metrics` | URL path for promxy's own metrics. |
| `--proxy-headers` | | Headers to forward from incoming requests to downstream server groups. Repeatable; also settable via `PROXY_HEADERS`. |

## Query engine

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--query.timeout` | `2m` | Max time a query may take before being aborted. |
| `--query.max-samples` | `50000000` | Max samples a single query may load into memory. |
| `--query.lookback-delta` | `5m` | Max lookback when retrieving metrics during evaluation. |
| `--query.max-concurrency` | `-1` (unlimited) | Max concurrently-executing queries. **Requires `--storage.path`** — the active query tracker needs a file on disk. |
| `--remote-read.max-concurrency` | `10` | Max concurrent remote read calls served by promxy's *own* remote_read endpoint. |

## Storage

Promxy has no TSDB. `--storage.path` is a working directory for the active
query tracker file and the `remote_write` WAL.

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--storage.path` | | Base directory for promxy's local working state. |
| `--storage.tsdb.path` | | **Deprecated** alias for `--storage.path`. Setting both is a fatal error. |

Without it the WAL goes to a temp directory removed on shutdown, so buffered
samples don't survive a restart.

## Rules and alerting

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--rules.alert.for-outage-tolerance` | `1h` | Max outage tolerated when restoring the `for` state of an alert. |
| `--rules.alert.for-grace-period` | `10m` | Minimum duration between an alert firing and its restored `for` state. Applies only to alerts whose `for` exceeds this. |
| `--rules.alert.resend-delay` | `1m` | Minimum time before resending an alert to Alertmanager. |
| `--rules.alertbackfill` | `false` | Recalculate alert state at startup by querying downstreams, for when the datastore has no `ALERTS_FOR_STATE` series. |
| `--alertmanager.notification-queue-capacity` | `10000` | Capacity of the pending-notification queue. |

See [Rules and alerting](../guides/rules-and-alerting.md).

## Shutdown

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--http.shutdown-delay` | `10s` | Time to keep serving after `SIGTERM` while failing health checks, so load balancers can drain. |
| `--http.shutdown-timeout` | `60s` | Max time to wait for in-flight requests during graceful shutdown. |

See [Running promxy](../operations/running.md#graceful-shutdown).

---

## `remote_write_exporter`

A companion binary (same container image) that receives `remote_write` and
re-exposes the most recent value of each series on `/metrics` — a convenient
target when you just want recording-rule and alert-state metrics scrapeable
again. See [Rules and alerting](../guides/rules-and-alerting.md).

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `--bind-addr` | `:8083` | Address to listen on. |
| `--write-path` | `/receive` | Path accepting protobuf remote_write. |
| `--write-text-path` | `/receive_text` | Path accepting the Prometheus text exposition format. |
| `--metrics-path` | `/metrics` | Path exposing the received series. |
| `--metric-ttl` | *(required)* | How long a series is retained after its last sample. |
| `--drop-stale` | `false` | Drop series written with a `StaleNaN`. |

Everything is in memory behind a TTL sweep; it is not a storage system.
