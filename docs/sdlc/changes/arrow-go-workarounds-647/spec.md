# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

Contract (`go/arrow_validate.go`):
- `validateInputArray` calls `array.ValidateFull` on the caller's array unless the type contains a NULL type, directly or through a dictionary's value type or a nested type's fields. REE is a nested type, so this includes a NULL values child under REE.
- When the type contains a NULL type, `validationData` gives each bufferless NULL node one nil buffer, as before. It no longer adds a slot to REE nodes. Arrow Go v18.9.0 validates a bufferless REE node itself and still validates its run ends, values, offset and length.
- Caller-owned array data is never modified.

Unchanged:
- Error messages, including Arrow's path prefixes such as `field "bad": ` and `list values: `.
- Ingest errors: `ingestColumn` still returns `StatusInvalidArgument` with `[redis] invalid Arrow input: <arrow message>` and no SQLSTATE.
- The NULL handling in `go/partition_retained_ipc.go`, the Map and sparse-union preflights in `go/pkg/input_preflight.c`, and the normalization budget.

Acceptance:
- New `TestCImportedRunEndNestedValidation` (`go/arrow_validate_cdata_test.go`, built only with cgo) sends each input through `cdata.ExportArrowArray` and `cdata.ImportCArray`. It asserts that the import has at least one REE node with zero buffers, so the test fails if a later import supplies the slot and stops testing this case.
  - Eight valid inputs are accepted, and their buffers are unchanged after validation: top-level sliced REE; REE with NULL values; REE in a struct; a sliced REE with NULL values in a struct; list of REE; map with REE values; sparse union of REE; dense union of REE.
  - Nine invalid inputs are rejected: zero first run end, decreasing run ends and too-short run ends, each at the top level, in a struct and in a list. Each gives the exact same Arrow message and path. Each also gives an `ingestColumn` error with the same status, message and SQLSTATE.
- The new test passes on the base commit, before the change, and after it.
- With the workaround removed and arrow-go v18.8.0 (before #1421), all 17 cases fail, as does the existing `TestCImportedRunEndValidation`.
- The existing REE, NULL and layout inventory tests pass unchanged.
