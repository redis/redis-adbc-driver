# Validation

November 12, 2026. See [intent](intent.md), [spec](spec.md), [plan](plan.md).
Implementation source: `8e178ba17f8775992ab77fc5710d1375528cd4da`.

## Reproduction and implementation

Before the fix, `TestSnapshotSQLAdmissionWaitsBeforeExecution` failed on live
Software: SQL completed successfully while another writer held the target's
admission lease. After the fix the same test waits, then updates both the
original row and the row published by the admitted owner. This checks that
execution acquires fresh state rather than just delaying stale work.

Managed SQL DML now calls the same admission implementation as Arrow append.
The existing control operation and transient leased pin field remain unchanged;
there is no storage-version change or automatic execution retry. Unmanaged
descriptors bypass the gate. Independent targets still publish while another
table's writer waits. The old Arrow test now publishes through its admitted
owner instead of issuing a new SQL statement that would correctly wait.

## Checks

All live checks use a dedicated, disposable Redis Software 8.2.6-25 database,
started with `make -C go redis-up`. Active 4x QPF, six Search workers, mnp routing,
32 connections, one RAM shard/noeviction and authenticated TLS/RESP3 are verified
before testing. Ordinary `make -C go test-software` rechecks the effective policy.
Go 1.27.3; four-CPU/eight-GiB container on Apple M5 Max arm64/36 GiB host RAM.

- Focused `go test -count=1 -race -v -p 1 -run
  '^TestSnapshot(SQLAdmission|IngestAdmission|WriteAdmission)' -timeout=4m .`:
  22 pass events, zero skips/failures/race reports. Covers concurrent INSERT,
  UPDATE, DELETE, MERGE, mixed SQL/Arrow, fresh-state evaluation, independent
  targets, cancellation, deadline, connection Close, uncertain admission replies,
  lease renewal/expiry and compaction admission. Sequence/input counters prove
  no SQL/Arrow replay and no evaluation while waiting.
- With `REDIS_SNAPSHOT_URI` set to this owned database, `go test -count=1 -json
  -p 1 -run '^(TestSnapshot|TestUnmanaged)' -timeout=20m .`: 686 pass events,
  75 conditional/opt-in skips, zero failures. Includes stale dependency/publication
  conflicts, retained operations, compaction, large writes, imports, ownership,
  unmanaged creation/writes and cleanup. Assertions/skips remain unchanged except
  the admitted-owner setup described above.
- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -json
  -p 1 -timeout=10m ./...`: 1327 test pass events, 1240 live/conditional skips,
  zero failures. These skips are not compatibility evidence.

- Ordinary Software coverage through `make -C go test-software`: 2263 unique
  passing test/subtest outcomes, 586 conditional skips; six packages pass and
  three have no applicable tests. All 1400 listed main-package top-level tests
  are accounted for. Coverage was completed across invocations: the initial run
  was interrupted at a synchronous race injection that waited on the newly shared
  gate. Five competing-replacement callbacks in two test files now publish through
  CREATE TABLE AS, a DDL operation outside table DML admission. They retain all
  stale-publication, row-ownership, pinned-reader and cleanup assertions. All 54
  affected race checks then passed with zero skips/failures. The 229 uncompleted
  top-level tests were explicitly selected for the continuation, which passed;
  a test-list/JSON audit verifies complete coverage and no remaining failed test
  outcomes. The interrupted package outcome remains in the private log; this is
  not presented as one clean uninterrupted full-suite invocation. No production
  change followed the initial implementation commit.

The Makefile-versioned native library/CLI build passes using
`VERSION=v0.0.26-dev.sql-admission`, and the binary reports Go 1.27.3.

## Native-library benchmark and evidence

The [benchmark report](../../../../benchmarks/table-modes/sql-admission-2026-11-12/report.md)
pins the implementation commit, GetInfo version and binary/source SHA-256.
After all live tests completed, the same dedicated Software fixture was verified
and resized to 4 GiB for the matched benchmark. The harness rechecked active QPF
and effective settings before every case and at completion. Python 3.15.2,
PyArrow 26.0.1, ADBC manager 1.13.0, redis-py 8.1.0, hiredis 3.5.0; Redis 8.8.0,
Search 8.8.8. Three measured repetitions and one warmup per case:

- All 12 timed existing-table workloads completed, 10K rows x 105 columns plus
  ordinal, up to 1000 rows per SQL INSERT/Arrow call. Complete values, physical
  Arrow types and multiplicities independently validated. No retries/conflicts.
- Median SQL seconds: one writer 8.478; four writers 19.449. Four-writer Arrow
  median 18.733 s, with a wide observed 10.669–19.553 s range. Same-table writes
  coordinate before execution; these measurements do not imply parallel speedup.
- Unchanged synchronized 100-row control, three trials per API: all four SQL and
  all four Arrow writers succeeded in every trial; all 100 visible rows checked.
  The earlier v0.0.25/v0.0.26 controls had three conflicting SQL writers per trial.
- A local benchmark launcher initially failed multiprocessing startup before any
  trial completed. Its missing main guard was corrected and a fresh work directory
  used. That attempt/log remain private; no completed measurements were discarded.
- `python3 benchmarks/table-modes/sql_admission_audit.py`: coverage, completion,
  configuration and recomputed arithmetic pass. Missing-trial, incomplete-write,
  changed-median and conflicting-control mutations are all rejected. Historical
  `arrow_audit.py` and `audit.py` also pass unchanged. New audit py_compile,
  gofmt and `git diff --check` pass.

Timing includes worker startup/imports, source loading/conversion, SQL literal
construction, connections, writes/publication and shutdown. Preparation and full
validation are outside timing. Only the affected managed writes/controls were
rerun; no new read, unmanaged/native, creation, 100M, physical Flex or Flight SQL
measurement is claimed. Shared workstation; ranges are observations, not confidence
intervals or an isolated release-version comparison.

Removed only this task's server, volumes and network after completion, verified
their absence, and removed its generated test environment file. Raw logs,
credentials, infrastructure identifiers, typed source and licenses stay private.

## Review scope

Reviewed per REVIEW.md for correctness/lifecycle, ownership/privacy and evidence.
The gate precedes workspace allocation and SQL evaluation; known busy admission
releases provisional pins and reacquires current state. Cancellation and unknown
transport outcomes terminate without execution replay; release retains existing
bounded cleanup. No published-row mutation, row merge or weakened conflict check.
Same-table participating writers serialize, so this does not promise parallel
write speedups, fairness or conflict-free arbitrary SQL. DDL, additional CTE
targets, changed read dependencies and older clients can still return 40001.
No human maintainer approval is asserted.
