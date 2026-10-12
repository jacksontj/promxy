# Configuration overview

Promxy takes one config file (`--config`, default `config.yaml`): a
**Prometheus configuration file** plus one extra top-level key, `promxy`.

```yaml
# ---- standard prometheus configuration ----
global: {}
rule_files: []
alerting: {}
remote_write: []

# ---- promxy-specific configuration ----
promxy:
  server_groups: []
  alert_templates: {}
```

Validate without starting up:

```
promxy --config=config.yaml --check-config
```

Annotated example: [`cmd/promxy/config.yaml`](../../cmd/promxy/config.yaml).

## The Prometheus half

Parsed by Prometheus' own config package, behaving as documented
[upstream](https://prometheus.io/docs/prometheus/latest/configuration/configuration/),
with these caveats.

### `global`

`evaluation_interval`, `external_labels`, and `query_log_file` all apply.
`external_labels` are added to everything promxy returns and are honoured by its
`/federate` handler.

Scrape settings (`scrape_interval`, `scrape_configs`) are meaningless; promxy
never scrapes.

### `rule_files` / `alerting`

Alerting rules evaluate across your **entire** infrastructure — a global
error-rate alert is trivial here and impossible on one Prometheus server.

Two differences from Prometheus:

- **Recording rules are rejected.** Promxy has no local TSDB. Configuring a
  recording rule without `remote_write` is a fatal config error.
- **Alerting rules without `remote_write` log a warning.** The alerts still
  fire, but the `ALERTS`/`ALERTS_FOR_STATE` series have nowhere to go.

See [Rules and alerting](../guides/rules-and-alerting.md).

### `remote_write`

With no local storage, `remote_write` *is* promxy's Appender: recording-rule
output and the `ALERTS` / `ALERTS_FOR_STATE` series all go here.

```yaml
remote_write:
  - url: http://localhost:8083/receive
```

One promxy-specific default: `queue_config.max_samples_per_send` is **100**,
not upstream's 2000. Large batches of high-cardinality recording-rule output can
decompress past the 32 MiB snappy limit Prometheus 3.5.3+ enforces on the
receiver, which then rejects the batch. Set it explicitly to override.

`--storage.path` makes the remote_write WAL durable across restarts; otherwise a
temp directory is used and removed on shutdown.

### `tls_server_config`

Parsed but **has no effect**; promxy's web server is configured entirely from
`--web.config.file`. See [Security](../guides/security.md).

## The promxy half

### `promxy.server_groups`

The bulk of promxy's configuration. Each entry is a set of Prometheus-API
endpoints holding the **same** data, merged and deduplicated by promxy;
different data belongs in different groups.

Full reference: [Server groups](server-groups.md).

### `promxy.alert_templates`

Optional. Customizes the `GeneratorURL` on alerts sent to Alertmanager.
Reference: [Alert templates](alert-templates.md).

## Reloading

Promxy reloads on `SIGHUP`, and on `POST /-/reload` with
`--web.enable-lifecycle`. Everything in the file is reloadable: server groups,
rules, alertmanagers, remote_write, alert templates. See
[Running promxy](../operations/running.md).
