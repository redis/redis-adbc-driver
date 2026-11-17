# Startup contract

Date: November 17, 2026. Base: v0.0.28. Status: implemented; see [validation](validation.md).
See [intent](intent.md) and [plan](plan.md).

The first EXISTS is an observation, not a lock. When a later index or database
size check observes nonempty state, re-read the catalog format before deciding
whether initialization is forbidden. A current-format catalog means another
opener has initialized the database. Run the existing format and write-capability
checks before proceeding. Server/deployment requirements remain mandatory.

Unknown transport/permission failures must propagate. Missing catalogs and old
formats must still be refused without modifying existing keys or indexes. The
atomic initialization script remains responsible for the final empty-database
check; no statement or uncertain mutation is replayed. Cluster node checks use
the routed catalog client, since the anchor exists only in its metadata slot.

Acceptance: deterministic stale-EXISTS cases for both index and DBSIZE guards;
legacy/old-format refusal tests; repeated focused tests with the race detector;
and dbt's parallel functional suite using a source-built library. Record skipped
capabilities and exact deployment provenance in validation.md.
