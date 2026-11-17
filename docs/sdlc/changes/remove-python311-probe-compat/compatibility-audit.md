# Other compatibility layers

Date: 2026-11-17. Scope: first-pass source audit on `d14eaf4e`, including Python,
Go/C bridge, packaging and integration configuration. Keyword matches were
inspected; this is not a proof that every compatibility path was found or is
reachable. Historical benchmark records and tests are distinguished from current
production behavior. Only the two Python 3.11 extraction workarounds are removed
by this change.

| Area | Location | Observed purpose / remaining question |
|---|---|---|
| SQLAlchemy 1.4/2.x | `python/adbc_driver_redis/sqlalchemy.py`: `type_compiler`, `dbapi`/`import_dbapi`, conditional multi-reflection methods | The declared Superset extra pins SQLAlchemy 1.4; the main SQLAlchemy extra requires 2.1. These branches have a declared consumer. |
| Older integration stack | `python/pyproject.toml`, `python/scripts/integration_registry.py` | Package minimum is Python 3.12; multiple integration profiles select 3.12. Superset pins SQLAlchemy <2 and PyArrow <19. These are explicit support choices needing a separate dependency/profile review. |
| Legacy table metadata | `go/store.go`: `migrateLegacyMetadata`, `tableMeta.resolve`; callers in `go/database.go` and `go/adopt.go` | Migration from v0.0.1 names and case-insensitive identifier fallback remain. Normal snapshot setup gates the migration branch; reachability and persistent-data requirements need review before removal. |
| Catalog identity fallback | `go/catalog_oid.go`: `legacyCatalogOID`, `tableOID`, `indexOID` | Supplies identities for metadata without recorded IDs, plus foreign/catalog objects. Not every use is merely support for old metadata. |
| Public report fields | `go/hashcheck.go`: `HashColumn.InPlaceType`; `go/snapshot_inspection.go` | Preserves `in_place_type` JSON/API naming while reporting current managed-import source types. Removal would change the public report schema. |
| ADBC ABI negotiation | `go/pkg/driver.go`: `AdbcDriverRedisInit` version switch; `go/pkg/adbc.h` | Explicit ADBC 1.0/1.1 ABI support with different function-table sizes. This is driver-manager interoperability. |
| Redis script command fallback | `go/snapshot_control_script.go`: `snapshotControlExecutor.runScript` | Uses EVAL after a definite NOSCRIPT or top-level EVALSHA command denial. Supports current ACL configurations; unknown outcomes remain terminal. |
| Old compaction workers | `go/snapshot_compaction.go`: legacy copied-image initial-scan path | Retains recovery for already populated unindexed outputs. Removal needs stored-job/recovery qualification. |

No other Python 3.11-specific runtime shim was found in the searched production
Python code. Optional-dependency ImportError handling and thread-local attribute
checks were inspected and are not classified as obsolete version compatibility.
This inventory does not authorize deleting persistent-data guards, public ABI
support, current ACL behavior or supported integration profiles.
