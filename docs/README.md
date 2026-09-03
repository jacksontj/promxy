# Promxy documentation

Promxy is an aggregating proxy that makes many shards of Prometheus appear as a
single Prometheus API endpoint. It requires no sidecars, no custom builds, and
no changes to your existing Prometheus infrastructure.

New here? Start with the [project README](../README.md) for the high-level
pitch and [MOTIVATION.md](../MOTIVATION.md) for the "why", then work through
[Getting started](getting-started.md).

## Getting started

- [Getting started](getting-started.md) — install, a minimal config, first query

## Configuration

- [Configuration overview](configuration/README.md) — the anatomy of the config file
- [Server groups](configuration/server-groups.md) — reference for every `server_groups` option
- [Command-line flags](configuration/cli-flags.md) — reference for every flag
- [Alert templates](configuration/alert-templates.md) — customizing alert `GeneratorURL`s

The annotated example config lives at
[`cmd/promxy/config.yaml`](../cmd/promxy/config.yaml); a worked multi-tenant
example is at [`cmd/promxy/multi_tenant.conf`](../cmd/promxy/multi_tenant.conf).

## Concepts

- [Architecture](concepts/architecture.md) — how a query flows through promxy
- [HA and merging](concepts/ha-and-merging.md) — `anti_affinity`, dedup, gap filling

## Guides

- [Multi-tenancy](guides/multi-tenancy.md) — Mimir/Cortex tenants, `inject_matchers`
- [Rules and alerting](guides/rules-and-alerting.md) — alerting rules, recording rules, `remote_write`
- [Native histograms](guides/native-histograms.md) — routing histogram queries losslessly
- [Label filtering](guides/label-filtering.md) — skipping downstreams that can't match
- [Security](guides/security.md) — TLS, auth to promxy and to downstreams

## Operations

- [Running promxy](operations/running.md) — endpoints, reloads, graceful shutdown
- [Metrics](operations/metrics.md) — what promxy exposes about itself
- [Troubleshooting](operations/troubleshooting.md) — common symptoms and their causes

## Contributing

- [Development](development.md) — building, testing, the Prometheus fork, vendoring

## Deployment

Deployment manifests live under [`deploy/`](../deploy):

- [`deploy/docker`](../deploy/docker) — docker-compose stack (promxy + VictoriaMetrics + Alertmanager)
- [`deploy/k8s`](../deploy/k8s) — plain Kubernetes manifests
- [`deploy/k8s/helm-charts/promxy`](../deploy/k8s/helm-charts/promxy) — Helm chart
