# Intent

2026-11-17. Requestor: Albert Wong. Implement
[#399](https://github.com/redis/redis-adbc-driver/issues/399) on source
`b63a010d88d2b69707594cb9dc063992dac06b32`. The user selected dedicated scope only
and explicit destructive consent with writers stopped. This supersedes the
earlier proposal-only deliverable; it does not approve a merge or release.

Allow Arrow consumers to replace application-owned schema/data with explicit
ownership and failure semantics. Preserve unrelated keys/indexes, managed atomic
replacement, unmanaged append/create, and DROP/GC external-resource ownership.
Exclude shared/online replacement, resume, rollback and new benchmark claims.

See [spec](spec.md), [plan](plan.md), [validation](validation.md) and the
[public contract](../../../unmanaged-arrow-replace.md).
