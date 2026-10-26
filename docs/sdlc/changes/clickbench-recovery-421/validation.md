# Verification evidence

2026-10-21; starting source 523adb1d75756b7a92a72820effb7e6949a07809.

- Python ClickBench discovery: **95 tests, 92 passed, 3 Linux-only skips** on
  macOS. Includes failure/duplicate/missing/nonfinite measurement gates,
  credential redaction and CLUSTER INFO preflight regressions.
- Go unit-only packages: `env -u REDIS_URI ... go test -count=1 -p 1 ./...`
  passed; Redis integration skips do not establish compatibility.
- Native Linux focused integration: `TestSQLParallelExactTopK|TestSharedScan`
  passed against a disposable loopback Redis with the pinned benchmark image.
- Full native Linux driver integration: `go test -count=1 -p 1 .` passed in
  123.011 seconds against disposable Redis. An earlier attempt found an ACL
  regression in optional range bounding; the corrected attempt passed. Runtime
  logs stay with the run. No integration tests targeted the benchmark dataset.
- Operational Python AST parsing and `git diff --check` passed.

Independent supplemental checks of original saved outputs confirm Q27 is a
mismatch, while Q32/Q33 are valid using exact source group membership plus the
complete reference's count-rank sequence. Q29 is valid under PostgreSQL regex
semantics: PostgreSQL defaults to dot matching newlines, whereas DuckDB requires
an explicit `s` option/inline flag. See [PostgreSQL documentation](https://www.postgresql.org/docs/17/functions-matching.html)
and [DuckDB documentation](https://duckdb.org/docs/current/sql/functions/regular_expressions).
Only the independent oracle uses `(?s)`; the timed SQL is unchanged. Original
standard-checker mismatches remain preserved. Do not describe the different
oracle semantics as a driver fix or silently compare differing product SQL.

Recovery evidence: all 10,000 ingestion batches mapped using surviving source
anchors; 2,081,569 missing rows with original row IDs materialized and preserved
in S3 (SHA256 `af05a6bef6b6887aeeb2ea4c0d9bba513009b10b280dab510357fa62cc1e1bc8`).
The failed primary has not yet passed full field verification at this record's
creation. Full rerun, both-API measurements, complete backup, verified restore
and cleanup are **pending**, not established by the tests above.

Current-main rebase validation (base 0a84794, fix 34cf41b): Python discovery
95 tests / 92 passed / 3 Linux-only skips; Go unit-only packages passed; full
native Linux driver integration passed in 121.238s on disposable pinned Redis.
The initial f7ae889 fix snapshot and its binary remain preserved separately.
A focused race pass gates the selected current-main build before live recovery.

Albert explicitly authorized coordinating a #406 performance window in the
“Create Redis ecosystem slides” chat. After fixed-run/API/backup/restore gates,
cleanup waits for that window's preserved report and release marker or its
06:55Z cutoff. Hard expiry stays 07:16:14Z. No performance timing starts before
the acceptance gates; the window may be shortened by measured runtime.

Current-main focused race integration passed in 1.278s. The complete native
statement/public-partition harness then passed **18 measurements / 9 independently
validated pairs** on a disposable 1,000-row, 105-column source fixture. This is an
SDK integration check, not full-scale performance evidence. The current-main
library SHA256 is `796573c9b971f92990fd569eb354f82cb5ffdc8eafeb50dbdf5c6353720a87cb`.


Targeted scope update: recovery completed at 2026-10-22T01:12:23Z; all 2,081,569 restored rows passed all105-field checks and all48 primaries reported99,997,497 documents. At Albert's request the fixed full sweep stopped after37 completed trials, preserving all raw outputs. Q24/Q25/Q27 now receive three trials each; the previously failed18 supported-scan measurements follow. Four focused local scope/acceptance tests pass, covering exact identities, omissions/duplicates/extras, execution versus correctness and strict cleanup status. Changed operational scripts parse and diff whitespace checks pass. Targeted live correctness and full-scale scans/backup/restore remain pending. No native code or timed binary changed in this scope update.


At02:30:46Z allnine Q24/Q25/Q27 trials passed independent correctness (six valid hot outputs, three eligible queries). At02:50:47Z narrow_full statement trial1 hit QueryDeadline at1,200s, with no observed Redis OOM; its already-running partition partner was retained under its original timing/cap. Four local resume tests pass, covering successful-partner retention, failed/nonfinite/mismatched output rejection, source/duplicate identity rejection and failed-attempt preservation. Changed Python scripts parse; full-scale resumed scans are still pending.
