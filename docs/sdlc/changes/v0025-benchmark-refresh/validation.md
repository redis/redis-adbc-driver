# Validation record

Date: 2026-11-12. Status: complete for the final user-authorized scope.
Source build: exact v0.0.25 release, `a8de1c3cce12138ae4c37cdbff1e8ed8875b3bc4`.

## Contract checks

- `make -C go build VERSION=v0.0.25` passed; ADBC GetInfo reports v0.0.25.
  Embedded Go toolchain is 1.27.3. Production driver/Python source is unchanged.
- A dedicated Software fixture was created through `make -C go redis-up`.
  Management readback verifies active factor 4, six Search workers, mnp routing,
  32 connections, one RAM shard and noeviction before benchmark timing.
- Actual server: Software 8.2.6-25 / Redis 8.8.0 / Search 8.8.8. TLS certificate
  and RESP3 negotiation verified. Task-owned local server: four CPUs/eight GiB;
  database: four GiB. Historical server settings are not relabeled.
- `python -m unittest discover -s benchmarks/clickbench -p 'test_*.py'`:
  124 tests ran, 121 passed, three opt-in Linux/systemd isolation skips (`CLICKBENCH_SYSTEMD_TEST` unset), zero failures.
  A first attempt lacked DuckDB; after installing the missing dependency in the
  private environment, the full suite passed. No assertions or skips changed.
- `env -u REDIS_URI go test -count=1 -p 1 -run
  '^TestSnapshotFlexExecutionRefusesBeforeDial$' .` from `go/` passed. This is
  a unit refusal check, not a live Flex performance result.

The first throughput attempt stopped after both the data and management TLS
endpoints began resetting connections. Its partial timings are not published.
Only the task-owned fixture was restarted; `make -C go redis-up` reverified
effective 4x QPF, active shard/Search readiness and TLS before rerunning the
affected suites. Completed earlier suites retain their original evidence.

## Scope and evidence

The user explicitly canceled the 100M rerun after backup-manifest inspection.
No full-scale restore, fresh ingestion or cloud provisioning followed. Its
historical presentation results remain unchanged. The separate future default
selects Flex stopgap, propagates that intent to isolated statement and partition
readers and rejects a resume with different storage intent. It does not bypass
v0.0.25's snapshot compatibility guard.

The original mutable-HASH loading harness cannot safely write into current
published managed snapshots. The rerun uses public Arrow/SQL operations for
managed tables and separate immutable native fixtures. A warmup validator first
rejected only Arrow nullability declarations; the corrected comparator checks
actual null counts, types, values and multiplicities before reporting success.
The final measured matrix starts after that harness correction.

Sanitized numerical evidence and arithmetic audit belong in
[the benchmark bundle](../../../../benchmarks/v0025-refresh/2026-11-12/README.md).
Operational records, infrastructure identifiers, credentials, datasets, binaries,
complete logs and native slide edit payloads are retained privately.

## Review

Correctness: report only completely consumed, validated benchmark cases and
complete predetermined repetition coverage. Expose timing boundaries and
metadata-normalization changes. Native writes omit managed snapshot work.

Ownership/security: native HSET never mutates published managed HASHes. Flushes
and cleanup are restricted to the task-owned local fixture. Existing server
containers and cloud backups are preserved; no cloud resources were created.

Evidence/design: compare within the new run; do not attribute changed timings
solely to the release. QPF stays at 4x. The new worker-policy experiment has a
different client placement and learning schedule, so the old 10/12 gate claim is
not reused. Preserve native slide tables, geometry and typography. The historical
Flight timings remain dated v0.0.18.

## Completed benchmark and presentation verification

All seven suites have complete validated repetition coverage: 16 loads, 84
incremental reads, 36 UNION memory executions, 144 IPC operations, 72 matched
transfers, 30 throughput trials and 36 worker-policy concurrency records. The
standard-library audit recomputes every summary and passes all coverage checks.

The existing native deck was edited in place. A native PPTX readback confirms
35 slides, nine updated tables, 58 expected text elements and ten updated speaker
note sections. All content checks pass. The three 100M slides, including their
notes, match the original content exactly. Element geometry is unchanged and
table font sizes remain at the template's 15 points.

The connector lacks `get_presentation`, so source/output native PPTX exports were
parsed for structural/style verification; a clearly labeled normalized model
was passed to the Slides issue checker. It found zero errors and only the same
two pre-existing template typography warnings. One closing-note replacement
needed a targeted repair after an earlier text replacement changed its match;
the subsequent native readback confirms the complete corrected notes. No table
or narrative text was rasterized.

The final native PDF exported 35 pages. Every page was inspected in ordered
contact sheets, with full-size checks of the performance overview and worker
policy slide. No clipping, overlap or geometry defects were found. No second
PDF render was required.

Final management readback still showed active 4x QPF, six Search workers, mnp
routing and 32 connections. The three task-owned local containers, their
anonymous volumes and the task network were removed; other containers and all
cloud backups were preserved. No dangling task endpoint remains in `test.env`.
Documentation links and `git diff --check` pass.
