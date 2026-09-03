# Running promxy

## HTTP endpoints

Promxy serves the full Prometheus web UI and v1 API, plus a few endpoints of its
own. Everything is under `--web.route-prefix` (which defaults to the path of
`--web.external-url`).

| Endpoint | Notes |
| -------- | ----- |
| `/` and the rest of the UI | The Prometheus UI, served from assets embedded by the `builtinassets` build tag. |
| `/api/v1/*` | The Prometheus v1 API — `query`, `query_range`, `series`, `labels`, `label/<name>/values`, `read`, `rules`, `alerts`, `targets`, … |
| `/api/v1/status/config` | Promxy's own handler, returning promxy's config rather than a Prometheus one. |
| `/api/v1/metadata` | Promxy's own handler, aggregating metadata across server groups. |
| `/federate` | Promxy's own federation handler, with a faster text encoder. |
| `/metrics` | Promxy's own metrics. Path configurable with `--metrics-path`. |
| `/-/ready` | Readiness. Returns `503` once promxy is shutting down. |
| `/debug/pprof/*` | Go pprof handlers. Restrict these if promxy is exposed. |
| `/-/reload` | Config reload. Only with `--web.enable-lifecycle`. |

Promxy also implements the **remote_read** API, so another Prometheus (or a
second promxy) can use promxy as a remote_read backend. It serves the
`STREAMED_XOR_CHUNKS` response type that a stock Prometheus client negotiates by
default. `--remote-read.max-concurrency` (default `10`) bounds concurrent reads.

## Layering promxy

Promxy aggregates Prometheus-compatible API endpoints, so a promxy can be a
downstream of another promxy. You can also mix implementations in one
deployment — Prometheus, promxy, VictoriaMetrics, Mimir, Thanos — since they all
expose a compatible API.

## Configuration reload

Two ways to reload:

```
kill -HUP $(pidof promxy)
```

```
curl -XPOST http://localhost:8082/-/reload    # requires --web.enable-lifecycle
```

Reloads are atomic and safe: promxy builds the entire new state (server groups,
discovery, rule manager, remote_write, alert templates), waits for every group
to become ready, and only then swaps it in and cancels the old one. If any part
fails to apply, the new state is discarded and the previous configuration keeps
serving.

Two metrics track this:

- `prometheus_config_last_reload_successful` — `1`/`0`
- `prometheus_config_last_reload_success_timestamp_seconds`
- `process_reload_time_seconds` — timestamp of the last `SIGHUP`

A failed reload also raises a banner in the UI via the notifications API, which
is cleared on the next success.

Validate before reloading:

```
promxy --config=config.yaml --check-config
```

## Graceful shutdown

On `SIGTERM` or `SIGINT`, promxy shuts down in stages:

1. Start failing `/-/ready` with `503`, and publish a "shutting down" notice to
   connected UIs.
2. Stop the alert notifier and the rule manager.
3. Sleep for `--http.shutdown-delay` (default `10s`) while still serving
   traffic. This is the drain window — it gives load balancers time to notice
   the failing health check and stop sending new requests.
4. Shut the HTTP server down gracefully, waiting up to
   `--http.shutdown-timeout` (default `60s`) for in-flight requests.

Match `--http.shutdown-delay` to your load balancer's health-check interval
times its unhealthy threshold, or you will drop requests during rollouts. Make
sure your orchestrator's termination grace period exceeds
`shutdown-delay + shutdown-timeout` — in Kubernetes that is
`terminationGracePeriodSeconds`, which defaults to 30s and is therefore *too
short* for promxy's defaults.

Note that `/-/quit` is routed by the embedded Prometheus web handler when
`--web.enable-lifecycle` is set, but promxy does not act on it. Use `SIGTERM`.

## Deployment shape

Promxy is stateless, so run several replicas behind a load balancer and let them
all share the same config.

The one caveat is **rules**. Every replica evaluates every rule independently, so
N replicas send N copies of each alert. Alertmanager deduplicates identical
alerts, so this is normally fine and is in fact how you get HA alerting — but it
does multiply the query load of rule evaluation across your downstreams, and it
multiplies recording-rule writes to `remote_write`.

## Resource notes

- **CPU** scales with query volume and with how much of each query promxy has to
  evaluate locally rather than push down. See
  [Architecture](../concepts/architecture.md#query-pushdown-nodereplacer).
- **Memory** scales with the raw data pulled back for locally-evaluated queries.
  `--query.max-samples` (default 50M) is the backstop.
- **File descriptors**: each server group keeps up to `max_idle_conns` (default
  20000) idle connections, `max_idle_conns_per_host` (default 1000) per host.
  Raise your process limits accordingly, or lower these if you have many groups.

## Deployment manifests

- [`deploy/docker`](../../deploy/docker) — docker-compose stack with
  VictoriaMetrics and Alertmanager
- [`deploy/k8s/promxy.yaml`](../../deploy/k8s/promxy.yaml) — plain Kubernetes
  manifests (namespace, RBAC for `kubernetes_sd_configs`, ConfigMap, Deployment)
- [`deploy/k8s/helm-charts/promxy`](../../deploy/k8s/helm-charts/promxy) — Helm
  chart, with optional PDB, HPA, VPA, ingress, and a configmap-reload sidecar
  that triggers promxy's reload when the config changes
