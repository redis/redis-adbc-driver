# Plan

2026-11-24; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Add `TestCImportedRunEndNestedValidation` in `go/arrow_validate_cdata_test.go`, behind `//go:build cgo` so builds without cgo still compile the package tests. Check that it passes on the base commit.
2. Remove the `RUN_END_ENCODED` conditions from `needsValidationPlaceholder` and `validationData` in `go/arrow_validate.go`. Update the comment to name the upstream fixes and #647.
3. Show that the test is sensitive: with the change and a temporary modfile pinning arrow-go v18.8.0, the REE cases must fail.
4. Run the unit pass (`cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`), the full Go suite on Redis Software with verified 4x QPF, and ADBC validation. Report skips.
5. Leave #647 open for the work that needs the next arrow-go release (see [intent](intent.md)).

No merge, release or human review approval is inferred.
