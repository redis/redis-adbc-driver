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

Discovery and row counts share one pinned committed generation. Concurrent DML,
TRUNCATE, DROP or recreation cannot mix table definitions or row counts within
one request. The returned Arrow data is materialized before the pin releases.

With `approximate=true`, native counts use immutable per-segment Search queries
with exact visibility masks: shadowed versions and tombstones never contribute.
This path requires FT.INFO readiness and FT.SEARCH access. Inexact numeric IDs or
excessive mask work fall back to authoritative directories. With
`approximate=false`, counts merge ordered row-ID directories and read winning
HASH identities and tombstones; they do not consult Search document counts.
Directory reads use bounded pages and require ZRANGE/HMGET plus normal snapshot
catalog/pin access. They have a one-minute per-table deadline and a two-million
physical directory-entry work budget, including shadowed/deleted entries.
Exhaustion fails explicitly rather than returning a partial count.

Both request modes retain `statistic_is_approximate=true` for API compatibility,
including native visible counts that are exact at their pinned generation.
NULL/distinct/min/max/width/size quantities remain unavailable and are never
represented by fabricated zeros. The full restricted-principal snapshot command
recipe remains a validation gate; historical read-only grants do not establish
current-protocol permission compatibility.

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
