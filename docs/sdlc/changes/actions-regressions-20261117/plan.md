# Plan and evidence

2026-11-17. Base: `3671fca7`. Status: local validation complete; replacement CI in progress.
See [intent](intent.md), [specification](spec.md).

1. Inspect the current failing workflow and preserve its failure signatures.
2. Stage competing work before foreground catalog admission; publish at existing
   pause boundaries, retaining conflict and lifecycle assertions.
3. Give Ray fixture fields explicit canonical SQL declaration metadata.
4. Validate sequentially on an isolated Redis Software fixture created with
   `make -C go redis-up`; setup verifies active factor 4, six Search workers,
   mnp routing and 32 connections. Keep generated operational artifacts private.
5. Review correctness/lifecycle, ownership, and evidence per REVIEW.md; push a
   focused PR and follow its replacement checks.

## Evidence

- Original paged ALTER fixture reproduced: timeout after 60.69 seconds, SQLSTATE
  57014 instead of required 40001, matching the nightly failure.
- Original Ray dataset test reproduced its metadata equality failure against
  the current native library.
- Fixed five Go fixtures (including all DROP/RENAME/rekey/temp variants): pass,
  root package 9.337s. Command: `make -C go test-software
  "TEST_ARGS=-run 'TestSQL(AlterTypeCleanupDuringCursor|RenameDuringDrop|DropDuringRename|RekeyDuringDrop)'"`.
- Complete Ray suite: **20 passed**, zero skips, three upstream Ray deprecation
  warnings; required TLS CA worker test enabled. Native library built with
  `make -C go build`. Command: verified Software runner executing
  `python -m pytest -q python/tests/test_ray_datasource.py`, with
  RUN_RAY_TESTS=1, REQUIRE_RAY_TLS=1, and fixture CA configured.
- Unit suite: `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test
  -count=1 -p 1 ./...` passed (root package 27.631s); integration skips expected.
- Public catalog concurrency/admission tests: `go test -race -count=1 -p 1
  -timeout=5m -v -run "TestSnapshot(Catalog|ConcurrentCatalog)" .` passed
  (4.540s), with REDIS_SNAPSHOT_URI set to the verified disposable fixture.
  Covers concurrent public SQL, Arrow wait/cancel/input consumption, lost
  admission replies, lease expiry and DML metadata dependencies.
- Audited the remaining native row-write hooks and found the same synchronous
  nested admission in CTAS, INSERT/Arrow ingestion, UPDATE/MERGE, DELETE and
  TRUNCATE fixtures. Staged their competing changes before the acknowledged-row
  boundary, retaining all existing publication, exact-image, identity, old-reader
  and no-replay assertions. The focused drop/truncate row-race selection passed;
  see the PR for final run links and timing.
- The superseded full-matrix driver logs confirmed a later nested-admission
  stall in TestSQLDropDuringCreateTableAs and exposed TestSQLRecordArrow's old
  metadata-free ExecuteSchema string expectation on both architectures. Updated
  the latter to verify exact RECORD metadata before its structural assertion.
- [PR CI](https://github.com/redis/redis-adbc-driver/actions/runs/44129203202)
  and [full amd64/arm64 dispatch](https://github.com/redis/redis-adbc-driver/actions/runs/44129357892)
  started; final results are tracked in [PR #455](https://github.com/redis/redis-adbc-driver/pull/455).

## Review

Reviewed per REVIEW.md: changes affect test fixtures only. Competing changes
are fully executed before the tested statement, so hooks publish prepared work
without reentering foreground admission. Session/rekey options and snapshot
ownership fences remain active; existing 40001, source-row and reclamation
checks remain intact. Ray uses explicit expected metadata and retains strict
equality. No permissions, production code or supported-environment changes.
Maintainer approval is not inferred; remaining evidence is tracked above.
