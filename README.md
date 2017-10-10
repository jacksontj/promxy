# promxy
A prometheus aggregating HTTP proxy.

Note: this is a WIP, at this point it should be considered PoC quality software.

## High-level goal
The goal is to create a prometheus proxy that makes many shards of prometheus
feel like a single cluster to the user. This is more-or-less necessary when operating
at scale -- as there are too many metrics for a single shard to handle. More importantly
the requirements around aggregation / query-ability don't change. Meaning there are on-demand
queries for data that need to be handled if the data exists.
