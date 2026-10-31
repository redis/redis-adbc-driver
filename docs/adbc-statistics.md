# ADBC statistics and constraints

`GetStatistics` implements the canonical ADBC 1.1 nested Arrow schema and
standard row-count key **6** (`adbc.statistic.row_count`). Each eligible table has one table-level statistic
(`column_name=NULL`), an int64 union value and `statistic_is_approximate=true`.
An empty eligible table returns zero; an unsupported quantity is omitted.
`GetStatisticNames` returns the canonical empty stream: the driver defines no
custom statistic keys. Standard keys do not belong in that enumeration.

Catalog, schema and table filters use the same case-sensitive SQL LIKE patterns
as object discovery (`%` and `_`). NULL means no filter; an empty string matches
only unnamed objects, of which this implementation has none. A missing catalog
produces zero catalog rows; a missing schema/table produces empty nested lists.
Views and temporary, adopted, application/foreign-index tables are omitted.
Moving tables and tables with pending index cleanup are also omitted.

With `approximate=true`, counts use live Search document counts and require
FT.INFO readiness plus FT.SEARCH access. With `approximate=false`, the driver
scans authoritative HASH identities, deduplicates keys and validates physical
row IDs; it does not consult Search counts. That path needs SCAN and HGET plus
normal catalog metadata access. It is bounded by a one-minute deadline, 64 MiB
identity budget and two million scanned keys. Exhaustion fails explicitly.

Both paths return `statistic_is_approximate=true`: neither isolates concurrent
DML/DDL, and SCAN can observe membership from different times. ADBC allows an
approximate result even when exact statistics were requested. The HASH path
avoids index lag; it does not provide a transaction snapshot. Quiescent sources
produce complete counts. Table identity changes fail rather than silently
switching to replacement data. NULL/distinct/min/max/width/size quantities are
unavailable and never represented by fabricated zeros.

Permission denial fails explicitly; it does not become a zero count. Named ACL
users do not require CONFIG for statistics. Reader-only retained
connections cannot discover source statistics. Context cancellation and deadlines
follow the [ordinary transport contract](driver-reference.md#cancelling-a-statement).

Object discovery continues to report stored PRIMARY KEY and CHECK names and
columns. PRIMARY KEY metadata describes declared key columns; it is not a claim
of enforced uniqueness. UNIQUE and FOREIGN KEY declarations/enforcement are
tracked by [#114](https://github.com/redis/redis-adbc-driver/issues/114), so
no UNIQUE, FOREIGN KEY or referenced-column usage metadata is invented here.
Constraint enforcement remains outside this metadata change.

The [2026-10-30 validation](sdlc/changes/210-212/validation.md) exercises native
metadata under independently authenticated read-only principals over verified
TLS, including separate Search/SCAN/source-key denials and constraint DDL.
The published `0.0.19` macOS ARM64 wheel passed those checks on Redis Software
8.6.2 with 4x QPF. Other release assets are not covered by that evidence.
