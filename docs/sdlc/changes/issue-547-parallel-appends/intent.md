# Parallel managed Arrow appends

Date: 2026-11-20. Initial investigation: `52b03c6f`; implementation rebased onto
`845c95b4`. Requestor: alberttwong. Author: Codex.
Status: implemented and locally verified; human review and hosted CI pending.
No human approval or release is recorded.

[Issue #547](https://github.com/redis/redis-adbc-driver/issues/547) requests
parallel input consumption and private staging into one managed table across
connections, database handles and client processes. Existing admission serializes
the entire input stream. Publication must keep every successful append complete
and preserve statement snapshots, without replaying failed or uncertain input.

Scope: append and create-append into an existing native managed table on Redis
Software, including the experimental Flex path. Initial creation, replacement,
SQL DML and DDL remain exclusive. Application-owned live/unmanaged writes keep
their existing ownership and partial-write contracts. This is not a transaction
API, full-scale ClickBench load, or a claim of improved throughput.

See the [specification](spec.md), [plan](plan.md), and
[verification](verification.md). No infrastructure identities, endpoint secrets,
private logs or license material belong in these records.

The same change corrects stale current read-mode documentation under
[issue #552](https://github.com/redis/redis-adbc-driver/issues/552). It distinguishes
ordinary concurrent queries, deferred source partitions, retained capture/replay
and internal aggregate workers by ownership and Software/Flex deployment. Read
feature requests #549–#551 remain separate work; this PR does not implement them.
