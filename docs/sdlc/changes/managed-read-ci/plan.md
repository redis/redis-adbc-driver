# Plan and evidence

See [intent](intent.md) and [contract](spec.md).

1. Inspect failing Actions jobs and reproduce the three failing test groups.
2. Select the Search path explicitly for cursor tests; retain a HASH positive
   control for unordered scans and add an ordered Search positive control.
3. Add directory-stream lifecycle coverage for single-table and UNION readers.
4. Run focused race tests, appropriate package tests, formatting and diff checks;
   record results here and submit a reviewable PR.

## Evidence

Before edits, the focused three-group selection reproduced all CI failures on
Redis Software 8.8.0, three shards. `make -C go redis-up` and `test-software`
verified active QPF scaling factor 4, six Search workers, mnp routing and 32
connections before tests. The full scan used HMGET, and unordered stream/UNION
readers correctly had no Search cursor.

After-change checks passed:

- `make -C go test-software TEST_ARGS='-race -run "^Test(SQLEmptyReadsNothing|SQLStreamResults|UnionAllStreamLifecycle|SQLDirectoryStreamLifecycle)"'`:
  all four selected groups passed with no race report or selected-test skips.
  Other packages reported no matching tests, as expected. This follows the same
  fixture verification above; it is not a full snapshot-suite run.
- In `go/`, `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  passed; endpoint-dependent integration tests skip in this unit-only pass.
- Changed Go files formatted with `gofmt`; `git diff --check` and local change
  record links passed.

Review against `REVIEW.md`: test-only diff preserves cursor assertions and
zero-read checks, verifies directory/pin cleanup and retained Arrow buffers, and
changes no production ownership or security behavior. No actionable findings.
GitHub CI pending. No maintainer approval or release claimed.

Review: [PR #409](https://github.com/redis/redis-adbc-driver/pull/409).
