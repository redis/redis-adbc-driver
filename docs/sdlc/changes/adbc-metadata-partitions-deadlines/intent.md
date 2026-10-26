# Intent

2026-10-22. Author: Codex; requestor: Albert Wong. Base: `6508626`.
Status: implemented; verification in progress; no maintainer approval inferred.
Requested work: [#210](https://github.com/redis/redis-adbc-driver/issues/210),
[#211](https://github.com/redis/redis-adbc-driver/issues/211),
[#212](https://github.com/redis/redis-adbc-driver/issues/212).

Give ADBC consumers useful, accurately scoped statistics, extend a bounded
retained-query operation family, and bound Redis I/O by caller deadlines.
Preserve honest partial-support matrix entries, autocommit write outcomes,
cursor non-replay and shared pool safety. Publish remaining coverage and assess
source-snapshot/temporary-source design before promising those capabilities.

Exclude new constraint enforcement, atomic snapshots, temporary partition
sources, generic remote server cancellation and release-wheel availability.
These unresolved issue requirements remain open and are recorded in the public
contracts. This PR is a scoped increment across the three issues.

See [spec](spec.md), [plan](plan.md), [evidence](evidence.md).
