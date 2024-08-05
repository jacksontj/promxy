# Promxy [![Test Actions Status](https://github.com/jacksontj/promxy/workflows/Go/badge.svg)](https://github.com/jacksontj/promxy/actions) [![GoDoc](https://godoc.org/github.com/jacksontj/promxy?status.svg)](https://godoc.org/github.com/jacksontj/promxy) [![Go Report Card](https://goreportcard.com/badge/github.com/jacksontj/promxy)](https://goreportcard.com/report/github.com/jacksontj/promxy) ![build](https://github.com/jacksontj/promxy/workflows/build/badge.svg) [![Docker Repository on Quay](https://quay.io/repository/jacksontj/promxy/status "Docker Repository on Quay")](https://quay.io/repository/jacksontj/promxy)

pronounced "promski" or präm-sē

## High-level overview
Promxy is a prometheus proxy that makes many shards of prometheus
appear as a single API endpoint to the user. This significantly simplifies operations
and use of prometheus at scale (when you have more than one prometheus host).
Promxy delivers this unified access endpoint without requiring **any** sidecars,
custom-builds, or other changes to your prometheus infrastructure.

## Why promxy?
[**Detailed version**](MOTIVATION.md)

**Short version**:
Prometheus itself provides no real HA/clustering support. As such the best-practice
is to run multiple (e.g N) hosts with the same config. Similarly prometheus has no real
built-in query federation, which means that you end up with N sources in grafana
which is (1) confusing to grafana users and (2) has no support for aggregation across the sources.
Promxy enables an HA prometheus setup by "merging" the data from the duplicate
hosts (so if there is a gap in one, promxy will fill with the other). In addition
Promxy provides a single datasource for all promql queries -- meaning your grafana
can have a single source and you can have globally aggregated promql queries.

## Quickstart
Release binaries are available on the [releases](https://github.com/jacksontj/promxy/releases) page.

If you are interested in hacking on promxy (or just running your own build), you can clone and build:

```
git clone git@github.com:jacksontj/promxy.git
cd promxy/cmd/promxy && go build -mod=vendor -tags netgo,builtinassets
```

An example configuration file is available in the [repo](https://github.com/jacksontj/promxy/blob/master/cmd/promxy/config.yaml).

With that configuration modified and ready, all that is left is to run promxy:

```
./promxy --config=config.yaml
```

## FAQ

### What is a "ServerGroup"?
A `ServerGroup` is a set of prometheus hosts configured the same. This is a common best practice
for prometheus infrastructure as prometheus itself doesn't support any HA/clustering. This
allows promxy to merge data from multiple hosts in the `ServerGroup` ([all until it becomes a priority](https://github.com/jacksontj/promxy/issues/3)).
This allows promxy to "fill" in the holes in timeseries, such as the ones created when upgrading
prometheus or rebooting the host

### What versions of prometheus does promxy support?
Promxy uses the `/v1` API of prometheus under-the-hood, meaning that promxy simply
requires that API to be present. Promxy has been used with as early as prom 1.7
and as recent as 2.13. If you run into issues with any prometheus version with the `/v1`
API please open up an issue.

### What version of prometheus does promxy use? And what does that mean?
Promxy is currently using a fork based on prometheus 2.24. This version isn't supremely important,
but it is relevant for promql features (e.g. subqueries) and sd config options.

### What changes are required to my prometheus infra for promxy?
None. Promxy is simply an aggregating proxy that sends requests to prometheus -- meaning
it requires no changes to your existing prometheus install.

### Can I have promxy as a downstream of promxy?
Yes! Promxy simply aggregates other prometheus API endpoints together so you can definitely layer promxy.
Similarly you can mix prometheus API endpoints, for example you could have prometheus, promxy, and 
VictoriaMetrics all as downstreams of a promxy host -- since they all have prometheus compatible APIs.

### What is query performance like with promxy?
Promxy's goal is to be the same performance as the slowest prometheus server it
has to talk to. If you have a query that is significantly slower through promxy
than on prometheus direct please open up an issue so we can get that taken care of.

**Note**: if you are running prometheus <2.2 you may notice "slow" performance when running queries that access large amounts of data. This is due to inefficient json marshaling in prometheus. You can workaround this by configuring promxy to use the [remote_read](https://github.com/jacksontj/promxy/blob/master/pkg/servergroup/config.go#L27) API.

### How does Promxy know what prometheus server to route to?
Promxy currently does a complete scatter-gather to all configured server groups.
There are plans to [reduce scatter-gather queries](https://github.com/jacksontj/promxy/issues/2)
but in practice the current "scatter-gather always" implementation hasn't been a bottleneck.

### How do I use alerting/recording rules in promxy?
Promxy is simply an aggregating proxy in front of your prometheus infrastructure. As such, you can use promxy to
create alerting/recording rules which will execute across your entire prometheus infrastructure. For example, if
you wanted to know that the global error rate was <10% this would be impossible on the individual prometheus hosts
(without federation, or re-scraping) but trivial in promxy.

**Note**: recording rules in regular prometheus write to their local tsdb. Promxy has no local tsdb, so if you wish
to use recording rules (or see the metrics from alerting rules) a [remote_write](https://github.com/jacksontj/promxy/blob/master/cmd/promxy/config.yaml#L22)
endpoint must be defined in the promxy config (which is where it will send those metrics).

### What happens when an entire ServerGroup is unavailable?
The default behavior in the event of a servergroup being down is to return an error. If all nodes in a servergroup
are down the resulting data can be inaccurate (missing data, etc.) -- so we'd rather by default return an error 
than an inaccurate value (since alerting etc. might rely on it, we don't want to hide a problem).

Now with that said if you'd like to make some or all servergroups "optional" (meaning the errors will
be ignored and we'll serve the response anyways) you can do this using the [ignore_error option](https://github.com/jacksontj/promxy/blob/master/cmd/promxy/config.yaml#L86) on the servergroup.

## Questions/Bugs/etc.
Feedback is **greatly** appreciated. If you find a bug, have a feature request, or just have a general question feel free to open up an issue!
