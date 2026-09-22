# Getting started

## Install

### Release binary

Prebuilt binaries: [releases page](https://github.com/jacksontj/promxy/releases).

### Container image

```
docker pull quay.io/jacksontj/promxy
```

Entrypoint is `/bin/promxy`; the image also ships `/bin/remote_write_exporter`.

### From source

```
git clone git@github.com:jacksontj/promxy.git
cd promxy/cmd/promxy && go build -mod=vendor -tags netgo,builtinassets
```

Both tags matter: `builtinassets` embeds the web UI (without it the UI errors),
`netgo` gives a static binary. See [Development](development.md).

## A minimal config

Promxy's config file is a **Prometheus config file** with an extra top-level
`promxy` key, so `global`, `rule_files`, `alerting`, and `remote_write` behave
as they do in Prometheus.

```yaml
promxy:
  server_groups:
    - static_configs:
        - targets:
            - prometheus-01:9090
            - prometheus-02:9090
      anti_affinity: 10s
```

Those two hosts are one *server group*: Prometheus servers scraping the **same**
targets with the **same** config. Promxy merges their data, filling gaps in one
from the other. See [HA and merging](concepts/ha-and-merging.md).

Validate before starting:

```
./promxy --config=config.yaml --check-config
```

## Run it

```
./promxy --config=config.yaml
```

Promxy listens on `:8082`. Open <http://localhost:8082> for the Prometheus UI,
or query directly:

```
curl 'http://localhost:8082/api/v1/query?query=up'
```

Point Grafana at `http://localhost:8082` as a Prometheus datasource for a single
aggregatable view of every server group.

## Adding more shards

Each shard is another server group. Add `labels` so results stay
distinguishable — this is what keeps `count(up)` correct:

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

Any Prometheus service discovery works in place of `static_configs`
(`consul_sd_configs`, `kubernetes_sd_configs`, `file_sd_configs`, …). The
targets discovered are the *Prometheus servers*, not scrape targets.

## Next

- [Configuration overview](configuration/README.md) — the rest of the config file
- [Server groups](configuration/server-groups.md) — every per-group option
- [Rules and alerting](guides/rules-and-alerting.md) — recording rules and alert
  state need a `remote_write` endpoint
- [Running promxy](operations/running.md) — endpoints, reloads, shutdown
