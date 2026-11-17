# Validation

2026-11-17. Source: `19ebcb46` plus the recorded working changes.
[Intent](intent.md), [spec](spec.md), [plan](plan.md).

Status: implemented and locally verified. This record supports closing
[#273](https://github.com/redis/redis-adbc-driver/issues/273) after the change
lands; it does not claim a released wheel or upstream Dask endorsement.

## Checks and actual outcomes

- `python -m pytest -q python/tests/test_dask_write.py python/tests/test_dask.py
  -k 'not distributed'`: **27 passed, 2 deselected**, no warnings. No server was
  used. These cover graph laziness, mandatory replay consent, exact columns,
  sink-task zero-retry annotations, conversion overflow/fraction/nullability
  refusal, handle cleanup, missing/partial acknowledgments, failure partition
  counts, serializable diagnostics and actual repeated-compute duplication.
- Both complete test files through `go/software-setup.sh --exec`, using absolute
  test paths because setup changes directory to `go/`: **29 passed in 3.85 s**,
  zero skips, failures or warnings. The setup gate reverified Redis Software
  8.8.0, three primary shards, active scaling factor 4, six effective Search
  workers, `mnp` routing with 32 connections, and `noeviction` before testing.
  Connections used authenticated trusted TLS/RESP3. Client: macOS arm64 through
  Docker published ports. Operational fixture identifiers, credentials and
  private endpoint paths are excluded.
- Native writer acceptance uses two distinct Distributed worker processes,
  checks their PIDs differ from the caller, and pins consecutive successful
  write evaluations to each worker. It verifies NULL/Unicode/empty partitions,
  exact acknowledged counts, and scheduler-side zero retries. A conversion
  failure after an acknowledged partition retains exactly its committed rows
  and prevents the next append. A native missing-target refusal preserves the
  existing destination. Fixture tables are dropped explicitly by the tests.
- Expanded native typed ingestion preserves a greater-than-2^53 integer,
  nullable decimal128(12,2), boolean, microsecond timestamp, date32 and binary
  containing actual zero and 0xff bytes. A fresh connection fetches all rows and
  checks exact Arrow table/type equality against an independent source table.
- Existing distributed reader coverage also passed in the complete run.
- Python 3.14.10; Dask/Distributed 2026.8.0; pandas 3.1.1; PyArrow 26.0.1;
  ADBC driver manager 1.13.0; pytest 9.2.0. Native library built from the working
  source containing this change. `compileall` and `git diff --check` passed.

The initial native launcher used root-relative paths after the setup script
changed directory and collected no tests. The corrected absolute-path command
produced the complete 29-test result above; the initial attempt is not counted
as a passing test run.

## Review and limits

Independent agent review inspected implementation, failure/count semantics,
resource lifecycle, task annotations, tests and documentation; no blocking
findings remained. This is agent review, not a human maintainer approval.

The sink explicitly requires acknowledgment of at-least-once task effects.
Dask worker-loss recomputation, scheduler overrides and repeated computation can
append duplicates. Neither zero ordinary retries nor a successful acknowledged
count proves exactly-once delivery. No worker-kill recovery guarantee, rollback,
resume, global worker-memory bound, external-writer coordination, automatic
cleanup, performance claim or release-wheel compatibility is established.
