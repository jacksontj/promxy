# Promxy [![Build Status](https://travis-ci.org/jacksontj/promxy.svg?branch=master)](https://travis-ci.org/jacksontj/promxy) [![GoDoc](https://godoc.org/github.com/jacksontj/promxy?status.svg)](https://godoc.org/github.com/jacksontj/promxy) [![Go Report Card](https://goreportcard.com/badge/github.com/jacksontj/promxy)](https://goreportcard.com/report/github.com/jacksontj/promxy)

## High-level overview
Promxy is a prometheus proxy that makes many shards of prometheus
appear as a single API endpoint to the user. This significantly simplifies operations
and use of prometheus at scale (when you have more than one prometheus host).
Promxy delivers this unified access endpoint without requiring **any** sidecars,
custom-builds, or other changes to your prometheus infrastructure.

## Quickstart
Release binaries are available on the [releases](https://github.com/jacksontj/promxy/releases) page.

If you are interested in hacking on promxy (or just running your own build), you can install via `go get`:

```
go get -u github.com/jacksontj/promxy/cmd/promxy
```

An example configuration file is available in the [repo](https://github.com/jacksontj/promxy/blob/master/cmd/promxy/config.yaml).

With that configuration modified and ready, all that is left is to run promxy:

```
./promxy --config=config.yaml
```

# Questions/Bugs/etc.
Feedback is **greatly** appreciated. If you find a bug, have a feature request, or just have a general question feel free to open up an issue!
