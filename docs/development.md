# Development

## Building

```
cd cmd/promxy && go build -mod=vendor -tags netgo,builtinassets
```

Both build tags matter:

- **`builtinassets`** embeds the Prometheus web UI assets. Without it the UI is
  broken at runtime.
- **`netgo`** uses the pure-Go resolver, producing a static binary.

`-mod=vendor` is required — dependencies are vendored (see below).

Release builds for all platforms:

```
make release
```

which runs [`build.bash`](../build.bash) for both `cmd/promxy` and
`cmd/remote_write_exporter`, writing to `build/`.

Container image:

```
docker build -t promxy .
```

The [`Dockerfile`](../Dockerfile) cross-compiles both binaries and produces an
Alpine image running as `nobody`.

## Testing

```
make test
```

which is:

```
go test -race -mod=vendor -tags netgo,builtinassets ./...
```

The `test/` directory holds the integration suite — PromQL correctness against
real downstreams, remote_read, remote_write, exemplars, federation — plus
benchmarks in `promql_bench_test.go`.

## Lint and formatting

CI fails on any of these, so run them before pushing:

```
make fmt           # gofmt -w -s
make imports       # goimports -local=github.com/jacksontj/promxy
make static-check  # staticcheck ./...
```

`make fmt` and `make imports` rewrite files in place; CI runs them and then
`git diff --exit-code`, so an unformatted tree fails.

`goimports` is configured with `-local=github.com/jacksontj/promxy`, which puts
promxy's own packages in their own import group after third-party ones. Match
the existing grouping in any file you touch.

Install the tools with:

```
go install golang.org/x/tools/cmd/goimports@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
```

## CI

[`.github/workflows/go.yml`](../.github/workflows/go.yml) runs fmt, imports,
staticcheck, and tests on every push to `master` and every PR.
[`.github/workflows/build.yml`](../.github/workflows/build.yml) builds the
multi-arch image.

The build cache is seeded from `master`: `master` runs write a fresh cache entry
keyed by run ID, and PR runs restore the most recent one but never write. So
every PR builds and tests off `master`'s warm cache and only recompiles what it
changed.

## The Prometheus fork

Promxy uses a **fork** of Prometheus rather than upstream, because it needs
hooks upstream doesn't expose — most importantly the PromQL engine's
`NodeReplacer`, which is how promxy pushes query fragments down to its
downstreams (see [Architecture](concepts/architecture.md)).

The fork is pinned via a `replace` directive in `go.mod`. To move to a new fork
version:

```
make update-prom-fork
```

which edits the `replace` to point at the new
`github.com/jacksontj/prometheus` tag and re-vendors. Update the tag inside the
Makefile target when bumping.

Bumping the fork usually also means re-vendoring the web UI assets — the
`mantine-ui` build has to be present in the fork *and* in promxy's `vendor/`, or
`builtinassets` builds produce a broken UI.

## Vendoring

Dependencies are vendored and committed. After changing `go.mod`:

```
make vendor
```

which runs `go mod tidy` followed by `go mod vendor`. Commit the `vendor/`
changes along with `go.mod`/`go.sum`.

[`go_mod_tidy_hack.go`](../go_mod_tidy_hack.go) exists to keep `go mod tidy`
from dropping dependencies that are only needed by the fork.

## Repository layout

| Path | Contents |
| ---- | -------- |
| `cmd/promxy` | The promxy binary; flags, wiring, the annotated example config |
| `cmd/remote_write_exporter` | Companion binary that re-exposes received remote_write on `/metrics` |
| `pkg/config` | Top-level config: Prometheus config + the `promxy` section |
| `pkg/servergroup` | Server group: discovery, per-target client stack, config |
| `pkg/promclient` | The API client decorators — merging, relabeling, filtering, error handling, matcher injection |
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

Two places are worth reading before changing query behaviour:

- `pkg/proxystorage/proxy.go` — `NodeReplacer`, and the rules governing when a
  query fragment may be pushed down. The comments there carry the reasoning for
  each non-obvious case.
- `pkg/promhttputil/merge.go` — `MergeSampleStream`, the anti-affinity merge.
  Subtle and heavily tested; change it with tests in hand.

## Contributing

Feedback, bug reports, and feature requests are all welcome — open an
[issue](https://github.com/jacksontj/promxy/issues).

For pull requests: keep `make fmt`, `make imports`, `make static-check`, and
`make test` green, and include tests for behaviour changes. If your change
touches merging or pushdown, say explicitly in the PR what correctness argument
makes it safe.
