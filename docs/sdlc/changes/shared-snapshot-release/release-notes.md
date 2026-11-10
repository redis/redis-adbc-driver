# Proposed v0.0.24 release notes

Status: prepared for review; **not a published release**. Production driver
source: `018e0368f2f08f5ffd6a51f4978f9d1eae9d8c9b`, including #369 and #373.
Do not represent this version as available until binary and PyPI publication
finish and released-artifact verification is recorded.

## Reusable native read snapshots

A native ADBC connection can retain one committed managed catalog generation
before discovery and reuse it across metadata calls and sequential SELECTs.
This supports the driver consistency boundary needed by multi-table full-refresh
extractors, including Airbyte's requested shared_snapshot mode.

Set `adbc.redis.read_snapshot_retention_ms` before acquisition (120000–3600000,
default 900000), then `adbc.redis.read_snapshot_action=acquire`.
`adbc.redis.read_snapshot_status` reports state, generation and a fixed deadline.
Release with `adbc.redis.read_snapshot_action=release` in a finally block after
complete consumption. A released parent permits already-active readers to finish
under their original view; expiry or lost authority fails without live fallback.
GetObjects validates the view even for catalog-only or nonmatching filters.

The same schemas, view definitions and managed values survive concurrent
supported publications, TRUNCATE/DROP and compaction. Native authentication and
ACL/SQL grant checks remain effective. There is no cross-connection/process
attachment or partition-worker support; writes, temporary objects, Substrait,
IPC and CUDA remain unsupported on a bound connection. Individual query and
resource limits still apply. This is not a write transaction or durable sync
checkpoint.

The feature retains storage protocol 17 and its existing fresh-storage/legacy
rejection requirements. It does not add distributed independent-shard snapshots
or upstream Redis native read views. Managed copies preserve captured values;
the earlier external capture retains its documented weaker consistency.

Flight SQL shared snapshots remain unsupported. The existing Airbyte connector
still rejects the setting; integrate the documented native lifecycle, pin the
released artifact and validate the connector before enabling it. A driver release
must not silently turn best-effort reads into a shared-snapshot claim.

Documentation: [API and Python example](https://github.com/redis/redis-adbc-driver/blob/main/docs/shared-read-snapshots.md).
Tracked acceptance: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
