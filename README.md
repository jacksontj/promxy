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

## FAQ

### What versions of prometheus does promxy support?
Promxy uses the `/v1` API of prometheus under-the-hood, meaning that promxy simply
requires that API to be present. Promxy has been used with as early as prom 1.7
and as recent as 2.2. If you run into issues with any prometheus version with the `/v1`
API please open up an issue.

### What changes are required to my prometheus infra for promxy?
None. Promxy is simply an aggregating proxy that sends requests to prometheus-- meaning
it requires no changes to your existing prometheus install.

### What is query performance like with promxy?
Promxy's goal is to be the same performance as the slowest prometheus server it
has to talk to. If you have a query that is significantly slower through promxy
than on prometheus direct please open up an issue so we can get that taken care of.


### How does Promxy know what prometheus server to route to?
Promxy currently does a complete scatter-gather to all configured server groups.
There are plans to [reduce scatter-gather queries](https://github.com/jacksontj/promxy/issues/2)
but in practice the current scatter-gather always implementation hasn't been a bottleneck.


## Questions/Bugs/etc.
Feedback is **greatly** appreciated. If you find a bug, have a feature request, or just have a general question feel free to open up an issue!
