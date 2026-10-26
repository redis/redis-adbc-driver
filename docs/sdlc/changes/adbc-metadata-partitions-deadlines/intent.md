# Intent

2026-10-22. Author: Codex; requestor: Albert Wong. Base: `6508626`.
Status: implemented; verification in progress; no maintainer approval inferred.
Requested work: [#476](https://github.com/alberttwong/redis-adbc-driver/issues/476),
[#477](https://github.com/alberttwong/redis-adbc-driver/issues/477),
[#478](https://github.com/alberttwong/redis-adbc-driver/issues/478).

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
