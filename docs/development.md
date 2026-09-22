# Development

## Building

```
cd cmd/promxy && go build -mod=vendor -tags netgo,builtinassets
```

Both tags matter: `builtinassets` embeds the web UI assets (without it the UI
is broken at runtime), `netgo` uses the pure-Go resolver for a static binary.
`-mod=vendor` is required; dependencies are vendored.

```
make release          # build.bash for both binaries -> build/
docker build -t promxy .
```

The [`Dockerfile`](../Dockerfile) cross-compiles both binaries into an Alpine
image running as `nobody`.

## Testing

```
make test   # go test -race -mod=vendor -tags netgo,builtinassets ./...
```

`test/` holds the integration suite (PromQL correctness against real
downstreams, remote_read, remote_write, exemplars, federation) plus benchmarks
in `promql_bench_test.go`.

## Lint and formatting

CI fails on any of these, so run them before pushing:

```
make fmt           # gofmt -w -s
make imports       # goimports -local=github.com/jacksontj/promxy
make static-check  # staticcheck ./...
```

`make fmt` and `make imports` rewrite in place; CI runs them then
`git diff --exit-code`, so an unformatted tree fails.

`-local=github.com/jacksontj/promxy` puts promxy's own packages in their own
import group after third-party ones. Match the existing grouping.

Install the tools:

```
go install golang.org/x/tools/cmd/goimports@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
```

## CI

[`.github/workflows/go.yml`](../.github/workflows/go.yml) runs fmt, imports,
staticcheck, and tests on every push to `master` and every PR.
[`.github/workflows/build.yml`](../.github/workflows/build.yml) builds the
multi-arch image.

The build cache is seeded from `master`: `master` runs write a fresh entry keyed
by run ID, PR runs restore the most recent one but never write. Every PR builds
off `master`'s warm cache and only recompiles what it changed.

## The Prometheus fork

Promxy uses a **fork** of Prometheus for hooks upstream doesn't expose, chiefly
the PromQL engine's `NodeReplacer` (see
[Architecture](concepts/architecture.md)).

The fork is pinned by a `replace` in `go.mod`. To move to a new version:

```
make update-prom-fork
```

This repoints the `replace` at a `github.com/jacksontj/prometheus` tag and
re-vendors; update the tag in the Makefile target when bumping.

Bumping usually also means re-vendoring the web UI assets: the `mantine-ui`
build must be present in the fork *and* in promxy's `vendor/`, or
`builtinassets` builds produce a broken UI.

## Vendoring

Dependencies are vendored and committed. After changing `go.mod`, run
`make vendor` (`go mod tidy` then `go mod vendor`) and commit `vendor/` along
with `go.mod`/`go.sum`.

[`go_mod_tidy_hack.go`](../go_mod_tidy_hack.go) keeps `go mod tidy` from
dropping dependencies only the fork needs.

## Repository layout

| Path | Contents |
| ---- | -------- |
| `cmd/promxy` | The promxy binary; flags, wiring, the annotated example config |
| `cmd/remote_write_exporter` | Companion binary that re-exposes received remote_write on `/metrics` |
| `pkg/config` | Top-level config: Prometheus config + the `promxy` section |
| `pkg/servergroup` | Server group: discovery, per-target client stack, config |
| `pkg/promclient` | API client decorators: merging, relabeling, filtering, error handling, matcher injection |
| `pkg/proxystorage` | `storage.Storage` implementation; `NodeReplacer` pushdown; histogram routing |
| `pkg/proxyquerier` | `storage.Querier` / `ChunkQuerier` over the server groups |
| `pkg/promhttputil` | Merge primitives, including the anti-affinity algorithm |
| `pkg/promapi` | Low-level Prometheus API client and decoding |
| `pkg/alertbackfill` | Recomputing alert state at startup (`--rules.alertbackfill`) |
| `pkg/alerttemplate` | Configurable alert `GeneratorURL` templates |
| `pkg/federate` | Promxy's `/federate` handler |
| `pkg/server` | HTTP server, TLS/web config, access logging |
| `pkg/middleware` | Header proxying (`--proxy-headers`) |
| `pkg/logging` | Bridges the Prometheus libraries' loggers onto logrus |
| `test/` | Integration tests and benchmarks |
| `deploy/` | docker-compose, Kubernetes manifests, Helm chart |

Read these before changing query behaviour:

- `pkg/proxystorage/proxy.go` — `NodeReplacer` and the pushdown rules. The
  comments carry the reasoning for each non-obvious case.
- `pkg/promhttputil/merge.go` — `MergeSampleStream`, the anti-affinity merge.
  Subtle and heavily tested; change it with tests in hand.

## Contributing

Bug reports and feature requests welcome — open an
[issue](https://github.com/jacksontj/promxy/issues).

For pull requests: keep `make fmt`, `make imports`, `make static-check`, and
`make test` green, and include tests for behaviour changes. If your change
touches merging or pushdown, state the correctness argument in the PR.
