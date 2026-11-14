# Specification

[Intent](intent.md), [plan](plan.md), [validation](validation.md).
Parent context: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).

In validateSnapshotTables, only a direct native NOPERM from the indexed-row query
proof may invoke the existing private initial-scan compatibility path. A wrapped,
joined or untyped error is terminal with its original diagnostic, without CREATE
or repeated query. A direct native FT.INFO permission refusal remains Unauthorized;
composed/untyped failures remain IO. Only the two exact direct native Search
inconsistent-state replies may cause another publication readiness poll.

Retain the original standalone ZCARD/FT.INFO execution and primary routing, all
pin/lease/directory/index/prefix/cardinality/ownership checks, final indexed-row
proof, native compatibility, publication and unknown-write handling. No storage
format, public option, permission cache or live-value cache changes.
