# Getting started

## Install

### Release binary

Prebuilt binaries for each release are on the
[releases page](https://github.com/jacksontj/promxy/releases).

### Container image

```
docker pull quay.io/jacksontj/promxy
```

The image also ships `remote_write_exporter` at `/bin/remote_write_exporter`;
the entrypoint is `/bin/promxy`.

### From source

```
git clone git@github.com:jacksontj/promxy.git
cd promxy/cmd/promxy && go build -mod=vendor -tags netgo,builtinassets
```

Both build tags matter: `builtinassets` embeds the web UI (without it the UI
returns errors), and `netgo` gives you a static binary. See
[Development](development.md) for the full build/test workflow.

## A minimal config

Promxy's config file is a **Prometheus config file** with an extra top-level
`promxy` key. That means `global`, `rule_files`, `alerting`, and `remote_write`
all behave exactly as they do in Prometheus.

The smallest useful config points at one group of Prometheus servers:

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets:
            - prometheus-01:9090
            - prometheus-02:9090
      anti_affinity: 10s
```

Those two hosts are one *server group*: a set of Prometheus servers scraping
the **same** targets with the **same** config (the standard Prometheus HA
pattern). Promxy merges their data, filling gaps in one from the other. See
[HA and merging](concepts/ha-and-merging.md).

Validate it before starting:

```
./promxy --config=config.yaml --check-config
```

## Run it

```
./promxy --config=config.yaml
```

Promxy listens on `:8082` by default. Open <http://localhost:8082> for the
Prometheus UI, or query the API directly:

```
curl 'http://localhost:8082/api/v1/query?query=up'
```

Point Grafana at `http://localhost:8082` as a Prometheus datasource and you get
a single, globally-aggregatable view of every server group.

## Adding more shards

Each additional *shard* of your infrastructure is another server group. Add a
`labels` block so results from different groups stay distinguishable — this
matters for label-less aggregations like `count(up)`:

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets: [prometheus-us-01:9090, prometheus-us-02:9090]
      labels:
        region: us
      anti_affinity: 10s

    - static_configs:
        - targets: [prometheus-eu-01:9090, prometheus-eu-02:9090]
      labels:
        region: eu
      anti_affinity: 10s
```

Every query is scatter-gathered to all groups and the results merged.

Any Prometheus service-discovery mechanism works in place of `static_configs`
(`consul_sd_configs`, `kubernetes_sd_configs`, `file_sd_configs`, …) — the
targets discovered are the *Prometheus servers*, not scrape targets.

## Where to go next

- [Configuration overview](configuration/README.md) — the rest of the config file
- [Server groups](configuration/server-groups.md) — every per-group option
- [Rules and alerting](guides/rules-and-alerting.md) — note that recording rules
  and alert-state metrics require a `remote_write` endpoint
- [Running promxy](operations/running.md) — endpoints, reloads, shutdown
