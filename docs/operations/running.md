# Running promxy

## HTTP endpoints

Promxy serves the full Prometheus web UI and v1 API plus a few of its own
endpoints, all under `--web.route-prefix` (defaults to the path of
`--web.external-url`).

| Endpoint | Notes |
| -------- | ----- |
| `/` and the UI | Prometheus UI, from assets embedded by the `builtinassets` build tag. |
| `/api/v1/*` | Prometheus v1 API — `query`, `query_range`, `series`, `labels`, `label/<name>/values`, `read`, `rules`, `alerts`, `targets`, … |
| `/api/v1/status/config` | Promxy's own handler, returning promxy's config. |
| `/api/v1/metadata` | Promxy's own handler, aggregating metadata across groups. |
| `/federate` | Promxy's own federation handler, with a faster text encoder. |
| `/metrics` | Promxy's metrics. Path set by `--metrics-path`. |
| `/-/ready` | Readiness. `503` once shutting down. |
| `/debug/pprof/*` | Go pprof. Restrict if promxy is exposed. |
| `/-/reload` | Config reload. Requires `--web.enable-lifecycle`. |

Promxy also implements **remote_read**, so another Prometheus (or promxy) can
use it as a remote_read backend. It serves the `STREAMED_XOR_CHUNKS` response
type a stock Prometheus client negotiates by default;
`--remote-read.max-concurrency` (default `10`) bounds concurrent reads.

Promxy can be a downstream of another promxy, and you can mix implementations —
Prometheus, promxy, VictoriaMetrics, Mimir, Thanos — since all expose a
compatible API.

## Configuration reload

```
kill -HUP $(pidof promxy)
curl -XPOST http://localhost:8082/-/reload    # requires --web.enable-lifecycle
```

Reloads are atomic: promxy builds the entire new state (server groups,
discovery, rule manager, remote_write, alert templates), waits for every group
to be ready, then swaps it in and cancels the old one. If any part fails to
apply, the new state is discarded and the previous config keeps serving.

Metrics:

- `prometheus_config_last_reload_successful` — `1`/`0`
- `prometheus_config_last_reload_success_timestamp_seconds`
- `process_reload_time_seconds` — timestamp of the last `SIGHUP`

A failed reload also raises a UI banner via the notifications API, cleared on
the next success.

Validate first with `promxy --config=config.yaml --check-config`.

## Graceful shutdown

On `SIGTERM` / `SIGINT`:

1. `/-/ready` starts returning `503`, and a "shutting down" notice goes to
   connected UIs.
2. The alert notifier and rule manager stop.
3. Promxy keeps serving for `--http.shutdown-delay` (default `10s`) — the drain
   window, letting load balancers notice the failing health check.
4. The HTTP server shuts down gracefully, waiting up to
   `--http.shutdown-timeout` (default `60s`) for in-flight requests.

Two things to get right:

- `--http.shutdown-delay` ≥ your health-check interval × unhealthy threshold,
  or you drop requests during rollouts.
- Your orchestrator's grace period must exceed
  `shutdown-delay + shutdown-timeout`. Kubernetes'
  `terminationGracePeriodSeconds` defaults to 30s — shorter than promxy's
  `10s + 60s`.

`/-/quit` is routed when `--web.enable-lifecycle` is set but promxy does not act
on it. Use `SIGTERM`.

## Deployment shape

Promxy is stateless: run several replicas behind a load balancer sharing one
config.

The caveat is **rules**. Every replica evaluates every rule, so N replicas send
N copies of each alert. Alertmanager deduplicates them — this is how you get HA
alerting — but it multiplies rule-evaluation query load on your downstreams and
recording-rule writes to `remote_write`.

## Resource notes

- **CPU** scales with query volume and with how much promxy evaluates locally
  rather than pushing down. See
  [Architecture](../concepts/architecture.md#query-pushdown-nodereplacer).
- **Memory** scales with raw data pulled back for local evaluation.
  `--query.max-samples` (default 50M) is the backstop.
- **File descriptors**: each group keeps up to `max_idle_conns` (default 20000)
  idle connections, `max_idle_conns_per_host` (default 1000) per host. Raise
  process limits, or lower these if you have many groups.

## Deployment manifests

- [`deploy/docker`](../../deploy/docker) — docker-compose with VictoriaMetrics
  and Alertmanager
- [`deploy/k8s/promxy.yaml`](../../deploy/k8s/promxy.yaml) — namespace, RBAC for
  `kubernetes_sd_configs`, ConfigMap, Deployment
- [`deploy/k8s/helm-charts/promxy`](../../deploy/k8s/helm-charts/promxy) — Helm
  chart with optional PDB, HPA, VPA, ingress, and a configmap-reload sidecar
