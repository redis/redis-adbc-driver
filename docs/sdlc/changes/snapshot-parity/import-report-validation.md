# Managed HASH import reports and remaining ACL/source-key gaps

Date: 2026-11-03. Parent source `8eb3cf6`. Import-report checkpoint;
[full parity](plan.md) remains incomplete. This historical checkpoint covers
protocol 12. The [protocol-13 repair](schema-namespace-validation.md) supersedes
its schema-isolation failure; the original measurements below are retained.

## Report fix and checks

`TestAdoptHashes` panicked at `Report.Checks[0]`: snapshot adoption returned no
checklist. Dry runs also missed independently created destination tables. The
managed-copy path now returns a complete checklist for dry runs, successful
Apply and refresh, with source/nested-key counts, type blockers, destination/
schema/view checks and explicit managed-copy/refresh semantics. Its public
checklist identifier is retained; the title describes the managed copy. Planned
commands retain selected source-index, encoding and other adoption options.

Dry runs never initialize or publish a catalog. Blocked Apply returns both its
report and an error, preserving the existing protection against replacing an
independently created table. Invalid typed values and empty/no-column sources
cannot publish. Application HASHes and indexes remain unchanged.

The CLI test retains output, row-count and source-preservation assertions. It
now verifies the agreed managed-copy contract: application changes stay invisible
until explicit refresh, refresh dry runs change nothing, Apply returns a complete
JSON report and the updated row count, and storage is private.

Verified Software/QPF4 strict-TLS checks:

- `go test -race -count=1 -v -run '^TestManagedHashImportReport' .`: two cases
  passed, 2.503 seconds. Dry runs, refreshes, collisions, invalid types, empty
  sources, source ownership and unchanged publication roots are covered.
- `go test -race -count=1 -v -run '^TestSnapshot(ExternalCopyCompatibility|ImportStartsBesideExternalData)$' .`:
  two cases passed, 3.063 seconds; existing blocked-Apply error assertions remain.
- `go test -race -count=1 -v -run '^TestAdoptCommand$' ./cmd/redis-adbc`:
  passed, 2.427 seconds, including refresh and JSON output.
- Final unit-only `go test -json -count=1 -p 1 ./...`: 896 pass events, 707
  expected integration skips, zero failures. Skips prove no compatibility.
- `gofmt`, Python validator formatting/help and `git diff --check`: passed.

The full `TestAdoptHashes` now gets past the panic and fails its first `__key`
lookup (`42703`): managed external copies currently lose the external key as a
queryable identity. That assertion was not deleted or skipped. Assertions about
live application changes and in-place writes also need reconciliation with
explicit-refresh managed ownership. This is not full adoption parity.

## Confirmed schema-isolation gap

The legacy ACL tests first fail in raw metadata helpers (`42P01`), before testing
restricted users. Moving a skip or broadening permissions would hide the issue.
A Software API principal probe confirmed direct access to another schema's row
with only `+ping +hgetall %R~adbc:* %R~public:*`. Protocol 12 stores row HASHes
under the same `adbc:*` grant as metadata. That recipe does not isolate row data.
The old read-only command list also lacks snapshot control/pin operations;
complete restricted-driver behavior still needs validation.

The durable [regression gate](../../../../scripts/snapshot_acl_scope.py) creates
unique owned SQL objects and API principals, validates metadata and public-row
access as positive controls, then requires a permission error for the other
schema's HASH. It removes its principals, mappings, ACL rules and SQL objects
even on failures. Private temporary credentials/configuration are deleted;
endpoints and identifiers are not emitted.

Run with the current native library, verified fixture URI and CA supplied privately:

```sh
python scripts/snapshot_acl_scope.py --driver /path/to/current/library \
  --software-api-port <owned-api-port> --software-data-port <owned-data-port> \
  --owned-software-fixture
```

Current result: **failed for the reproduced regression**, reporting
`schema_isolated: false` and `owned_resources_removed: true`. Both positive
controls passed. Software/noeviction, strict TLS and active effective 4x QPF were
verified. A preceding private probe independently reproduced the result. This
must remain a failing gate until isolation is restored, not a skip or passing
ACL claim. The reference and snapshot guide now describe the legacy recipe's
actual boundary.

## Author review and next work

Reviewed preflight/publication outcomes, no-write dry runs, blocked Apply,
refresh identity, source ownership, CLI/JSON behavior, credentials and cleanup.
Self-review is not maintainer approval. Restore schema-specific physical row
access while preserving references, GC, fences, compaction and partition recovery;
then validate complete restricted-reader/writer recipes using Software API
principals. Preserve imported `__key` identity and complete adoption/sweep/ACL
regressions. Platform/Cluster/recovery and large-operation/performance work remain.
