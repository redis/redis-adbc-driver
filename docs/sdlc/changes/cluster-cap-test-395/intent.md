# Isolate runtime Search-cap tests on OSS Cluster

Date: 2026-10-21. Author: Codex; requestor: Albert Wong (continue). Source:
1674ff7, merged #394. Status: implemented, validation in progress, review pending.
Issue: [#180](https://github.com/redis/redis-adbc-driver/issues/180).
See [spec](spec.md) and [plan](plan.md).

An opt-in full Go run with REDIS_PARTITION_CAP_TEST=1 passed the partition test
but later failed many SQL tests with LIMIT exceeds maximum of 3/137. Keyless
CONFIG through ClusterClient routed changes and restoration to different nodes.
Preserve exact row assertions while restoring every node's configuration; require
this regression in both OSS CI architectures. This changes test isolation and
coverage, not product execution or Software's configured-cap contract.
