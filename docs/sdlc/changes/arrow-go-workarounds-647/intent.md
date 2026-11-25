# Intent

Requested 2026-11-24: fix [#647](https://github.com/redis/redis-adbc-driver/issues/647). Base: `ea16062c` (main). Status: local change, first part of the issue only. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

The driver carries several workarounds for Arrow Go v18.9.0 validation and C Data behavior. They cost extra copies and code, and they can hide upstream regressions once the upstream fixes ship. #647 lists them with their upstream fixes.

This change does the part of #647 that v18.9.0 already allows. [apache/arrow-go#1421](https://github.com/apache/arrow-go/pull/1421) (in v18.9.0) lets `array.ValidateFull` accept a run-end-encoded (REE) parent that has no buffer slot. The issue had checked only a top-level REE array. This change tests REE nested in other types too, and then removes the REE conditions from the validation adapter in `go/arrow_validate.go`.

Affected consumers: anyone who binds or ingests Arrow input that contains REE columns, through C Data (the shared library, Python and DuckDB) or the Go API. Accept/reject outcomes, error messages, status codes and SQLSTATEs must not change.

Desired:
- Tests that send REE through a real Arrow C Data export and import, at the top level and inside a struct, list, map value, sparse union and dense union. They include REE with a NULL values child, plus invalid run ends at the top level and inside a struct and a list.
- REE input reaches `array.ValidateFull` without the driver's placeholder.
- The NULL placeholder stays, including for a NULL values child under REE.

Excluded until an arrow-go release contains apache/arrow-go#1550, #1570 and #1600 (none as of 2026-11-24; the latest release is v18.9.0 from 2026-11-16):
- bumping `github.com/apache/arrow-go/v18`;
- removing the NULL placeholder in `go/arrow_validate.go` and `go/partition_retained_ipc.go`;
- removing the Map-key preflight in `go/pkg/input_preflight.c`;
- re-qualifying #460.

Also excluded: the sparse-union preflight, which stays until the importer itself returns errors for those inputs. #647 stays open for this remaining work.
