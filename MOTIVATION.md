## Promxy, a prometheus scaling story

### The beginning
Prometheus is an all-in-one metrics and alerting system. The fact that everything is built-in is quite convenient when doing initial setup and testing. Throw grafana in front of that and we are cooking with gas! At this scale -- there where no concerns, only snappy metrics and pretty graphs.

### Redundancy
After installing the first prometheus host you realize that you need redundancy. To do this you stand up a second prometheus host with the same scrape config. At this point you have 2 nodes with the data, but grafana only pointing at one. Quickly you put a load balancer in front and grafana load balances between the 2 nodes -- problem solved! Then some time in the future you have to reboot a prometheus host. After the host reboots you notice that you have holes in the graphs 50% of the time. With prometheus itself there is no solution short or long term for this as there is no cross-host merging and prometheus' datastore doesn't support backfilling data.

### Sharding
As you continue to scale (adding more machines and metrics) you quickly realize that
all of your metrics cannot fit on a single host anymore. No problem, we'll shard the
scrape config! The suggested way in the prometheus community is to split your metrics
based on application -- so you dutifully do so. Now you have a cluster per-app for
metrics separate. Soon after though you realize that there are lots of servers, so
its not even feasible for all the app metrics to be in a single shard -- you need to 
split them. You do this by region/az/etc. but now grafana is littered with so many
prometheus datasources! No problem you say to yourself, as you switch from using
a single source to using mixed sources -- and adding each region/az/etc. with the
same promql statements.

### Aggregation
As you've settled into running prometheus in an N shard by M replica setup (with N
selectors in grafana) and having to duplicate promql statements in grafana -- you
realize that you have some metrics/alerts that need to be global (such as global QPS,
latency, etc.). And here you realize that it cannot be done with the infrastructure you
have setup! This is no problem though, as you read through the prometheus community
forums etc. and realize that you are supposed to set up a global aggregation shard of
prometheus to scrape all your other prometheus instances. You set this up, but then
realize that the recommendations are to summarize data (which makes sense, since you
cannot just shove N hosts worth of data onto 1). Since these are global metrics you
rationalize that it is okay to drop some granularity for the sake of having them and
then you look over the rest of the cluster only to realize that this federation is
accounting for 90%+ of all queries to prometheus -- meaning you now have to uplift your
other existing shards just to get your global metrics.

### Despair
At this point you consider your situation:

- You have metrics
- You have redundancy (with the occasional hole on a restart or node failure)
- You have **many** prometheus data sources in grafana (which is confusing to all your grafana users -- as well as yourself!)
- You have aggregation set up -- which (1) accounts for the majority of load on the other promethes hosts (2) is at a lower granularity than you'd like, and (3) now means that you have to maintain separate alerting rules for the **aggregation** layers from the rest of the prometheus hosts.

And you tell yourself, this seems too complicated; there must be a better way!

### Promxy
You google around (or ask a friend) and you find out about this tool -- promxy
(what a silly name). You set it up and you immediately able to solve your pain points:

-  no more "holes" in metrics
-  single source in grafana
-  no need for aggregation layers of prometheus anymore!

In addition to solving the pain points you get access logs (which you didn't even
put on the list, but admit it: you've been missing them).
