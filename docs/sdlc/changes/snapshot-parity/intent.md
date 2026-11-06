# Intent: snapshot feature and performance parity

Date: 2026-11-02. Requestor: user. Parent: b4b73e0.
Status: in progress. PR: https://github.com/redis/redis-adbc-driver/pull/284

Complete the remaining snapshot feature and performance parity work, including
shared-generation live partitions, visibility-correct Search/parallel read paths,
metadata/publication and index overhead, large-operation admission and compaction,
and complete feature/platform regression checks with comparative benchmarks.
Preserve the agreed managed-copy/explicit-refresh behavior, no migration,
autocommit-only scope, Active-Active exclusion and noeviction requirement.

Performance parity must be measured against a stated baseline/workload after
complete result validation. A fallback can preserve semantics without establishing
performance parity. Record gaps rather than marking the entire goal complete
when only a subset is implemented. See [spec](spec.md) and [plan](plan.md).
