# Deferred reads of unmanaged application data

Date: 2026-11-20. Base: `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implemented and locally verified; awaiting human review; no release recorded.

[Issue #549](https://github.com/redis/redis-adbc-driver/issues/549) requests
independently schedulable ADBC partition reads for existing unmanaged registrations.
The application owns both HASHes and Search indexes. Ordinary SQL already reads
those sources, but default deferred partitions require managed numeric row IDs.

Deliver a bounded opt-in path for independent connections/processes, preserving
native source-index membership and exact HASH values without changing ownership.
Source values remain live; this work does not introduce a source snapshot,
immutable retries, retained output, automatic worker scheduling or a speedup claim.

See the [specification](spec.md), [plan](plan.md), and [verification](verification.md).
