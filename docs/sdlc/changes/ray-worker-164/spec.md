# Ray worker reader contract

Date: 2026-10-29. Author: Codex. Baseline: `34ae626`.
Status: implemented; human review pending.
See [intent](intent.md), [plan](plan.md) and [validation](validation.md).

`adbc_driver_redis.ray.read_query(uri, query, *, parameters=None,
partition_count=8, batch_rows=65536, concurrency=None, partition_scan_cap=0,
db_kwargs=None, conn_kwargs=None)` returns a Ray Dataset. The optional `ray`
extra pins Ray 2.59.0; Python 3.12+ follows the package minimum.

Planning immediately executes the public partition API in explicit live mode,
retains opaque byte descriptors and Arrow schema, then closes its connection.
Unsupported live queries fail explicitly. Each descriptor becomes one ReadTask;
Ray parallelism/limit hints do not change descriptor membership. Concurrency
controls simultaneous tasks independently of partition count (1..1024).
Workers authenticate independently and own the connection, cursor and Arrow
reader through iteration. Each batch yields an Arrow table with the planned
schema; empty reads emit a typed empty table and task metadata supplies schema.
The driver batch size bounds batch rows, not Ray pipeline memory.

Native DB-API and Arrow IO errors become RuntimeError with their original
message because ADBC exceptions do not round-trip through Ray serialization.
Context unwinding closes resources on errors, generator close and cooperative
cancellation. Forced process death can bypass cleanup; cursor expiry remains.

No shared snapshot, stable retry result, output ordering, exactly-once or CDC
claim. Schema/table changes can invalidate a plan. Connection configuration and
descriptors travel through Ray; use a trusted cluster, protect credentials and
install matching libraries and TLS trust files on every node. The existing
partition ACL, Search readiness, cap and query-subset contracts apply.

Acceptance: real process execution and descriptor serialization; static complete
nonoverlapping coverage; typed NULL and empty results; bounded batch yields;
close/failure/cancellation cleanup; replay/live changes; actual Ray task retry;
verified TLS and missing-trust rejection. Multi-node and release-wheel claims
require separate evidence. Upstream interface direction remains open in #164.
