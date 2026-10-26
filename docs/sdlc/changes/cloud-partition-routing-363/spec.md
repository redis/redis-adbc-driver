# Contract

Date: 2026-10-21. Source: merged #407. Author: Codex; requestor: Albert Wong (keep going; Redis Cloud Pro by default). Refs [#170](https://github.com/redis/redis-adbc-driver/issues/170).

Require real management readback of at least two primary shards before claiming sharded coverage. Preserve all shared checker assertions: full Arrow values/schema, Search and direct HASH reads, denials, lifecycle and TLS. Keep keys in the parent process and touch only newly owned resources. Record routing mode explicitly; a proxy run alone does not prove OSS Cluster API routing. Delete paid resources and verify absence. See [intent](intent.md) and [plan](plan.md).

Cluster API evidence additionally requires a complete primary slot map and independently checks that all 1,099 owned live fixture rows map to advertised primary owners with rows on every owner. No endpoint/private data is retained in these counts. API compatibility and proxy compatibility are separate runs.

Observed Cloud topology: two management-reported backend shards expose one external proxy slot owner even with external Cluster API enabled. Keep backend-shard and advertised-owner requirements independent. Do not require a service topology it does not expose or describe the result as direct backend routing.
