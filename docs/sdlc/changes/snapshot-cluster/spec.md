# Specification: Cluster API and recovery compatibility

Date: 2026-11-02. Parent: 2734c57. Status: implemented; validation recorded separately.
See [intent](intent.md), [plan](plan.md), and PR #284.

Validate noeviction and primary/non-Active-Active access on every primary.
Initialize only after checking all primaries for existing keys/indexes, except
explicit external imports, which reject existing legacy driver metadata.
All snapshot keys and index names keep the catalog hash tag. Node clients used
by direct Search and transactions must install the same snapshot hooks, including
nodes learned after a topology change. Maintenance must retain the connection's
Cluster API mode. Metadata copying and reclamation use exact score-zero key
directories with
lexicographic continuation, avoiding SCAN cursor invalidation across restart or
slot moves. Committed directory counts detect an incomplete manifest. Storage
protocol 6 rejects 1–5 without migration. Distributed Search schema commands run
outside Lua with pre/post server admission; late unreferenced indexes are reclaimed.
Rename/rekey allocates a private replacement under the catalog tag. Uncertain
writes/cursor reads are never replayed.

Verify full result values and reference lifetimes on an owned three-node Software
deployment, three primary shards plus replicas, TLS, active verified 4x QPF and
AOF fsync-always. Exercise primary promotion and restart with persisted storage.
Record actual availability/consistency behavior, including explicit transient
errors; do not promise transparent mid-stream cursor recovery or stronger Redis
durability than the configured server provides.
