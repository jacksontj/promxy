# Configuration overview

Promxy takes a single config file (`--config`, default `config.yaml`). That
file is a **Prometheus configuration file** with one additional top-level key:
`promxy`.

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

Validate a config without starting up:

```
promxy --config=config.yaml --check-config
```

The fully annotated example config is
[`cmd/promxy/config.yaml`](../../cmd/promxy/config.yaml).

## The Prometheus half

These sections are parsed by Prometheus' own config package and behave as
documented [upstream](https://prometheus.io/docs/prometheus/latest/configuration/configuration/),
with the caveats below.

### `global`

`evaluation_interval`, `external_labels`, `query_log_file`, and
`query_log_file`-adjacent settings all apply. `external_labels` are added to
everything promxy returns, and are honoured by promxy's `/federate` handler.

Scrape-related settings (`scrape_interval`, `scrape_configs`) are meaningless —
promxy never scrapes anything.

### `rule_files` / `alerting`

Alerting rules work and evaluate across your **entire** infrastructure, which is
their main appeal: a global error-rate alert is trivial in promxy and impossible
on an individual Prometheus server.

Two things differ from Prometheus:

- **Recording rules are rejected.** Promxy has no local TSDB. Configuring a
  recording rule without `remote_write` is a fatal config error.
- **Alerting rules without `remote_write` log a warning.** The alerts still
  fire, but the `ALERTS`/`ALERTS_FOR_STATE` series have nowhere to go.

See [Rules and alerting](../guides/rules-and-alerting.md).

### `remote_write`

Promxy has no local storage, so `remote_write` *is* promxy's Appender. Anything
promxy would "write" — recording rule output, `ALERTS` and `ALERTS_FOR_STATE`
series from alerting rules — is sent here.

```yaml
remote_write:
  - url: http://localhost:8083/receive
```

One promxy-specific default: `queue_config.max_samples_per_send` defaults to
**100**, not upstream Prometheus' 2000. Large batches of high-cardinality
recording-rule output can decompress past the 32 MiB snappy limit that
Prometheus 3.5.3+ enforces on the receiving side, and the receiver then rejects
the whole batch. Setting `max_samples_per_send` explicitly overrides this.

If you set `--storage.path`, the remote_write WAL is durable across restarts;
otherwise a temporary directory is used and removed on shutdown.

### `tls_server_config`

A top-level `tls_server_config` key is accepted by the parser but **has no
effect** — promxy's web server is configured entirely from `--web.config.file`.
Use that instead; see [Security](../guides/security.md).

## The promxy half

### `promxy.server_groups`

The bulk of promxy's configuration. Each entry is a set of Prometheus-API
endpoints holding the **same** data, which promxy merges and deduplicates.
Different data belongs in different groups.

Full reference: [Server groups](server-groups.md).

### `promxy.alert_templates`

Optional. Customizes the `GeneratorURL` promxy puts on alerts sent to
Alertmanager. Reference: [Alert templates](alert-templates.md).

## Reloading

Promxy reloads its config on `SIGHUP`, and on `POST /-/reload` when started with
`--web.enable-lifecycle`. Everything in the file is reloadable — server groups,
rules, alertmanagers, remote_write, alert templates. See
[Running promxy](../operations/running.md).
