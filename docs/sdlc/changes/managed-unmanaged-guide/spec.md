# Specification

The [guide](../../../managed-vs-unmanaged-tables.md) must distinguish application
ownership from driver ownership; compare schema, consistency, freshness and
maintenance; and explain explicit refresh, source preservation and capture limits.
It must not imply live unmanaged SQL tables, write-back, multi-statement write
transactions or coordinated cross-shard snapshots.

Use current [snapshot contracts](../../../snapshots.md) and
[shared read contracts](../../../shared-read-snapshots.md) as authority over
historical in-place adoption descriptions. See [intent](intent.md) and [plan](plan.md).
