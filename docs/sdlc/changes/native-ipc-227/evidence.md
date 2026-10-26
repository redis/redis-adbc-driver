# Verification and review: native IPC

Date: 2026-10-22. Author/reviewer: Codex (agent review, not human approval).
Base: `9493c647aee33916cfc32840b9ba517388e51b50`.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Checks run

- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all three packages passed.
  Redis-dependent Go integration tests skipped by design; this is a unit-only
  pass. This full pass preceded the final IPC cancellation-status adjustment;
  the focused IPC Go tests passed again after that adjustment.
- `env -u REDIS_URI go test -race -count=1 -p 1 -run TestIPC ./`: passed.
  Checked allocator tests cover stream/file buffers, empty and multi-batch
  results, early input release, failed/late export and cancellation cleanup.
- `make -C go build`: passed, including a rebuild after the cancellation fix.
- `PYTHONPATH=python REDIS_ADBC_LIBRARY=$PWD/go/build/libadbc_driver_redis.dylib
  REDIS_URI=redis://127.0.0.1:26379/0 /private/tmp/redis-adbc-issues-venv/bin/python
  -m pytest -q python/tests/test_ipc.py`: **8 passed** on the compiled C library
  and existing local Redis Software endpoint. Unique fixture tables were
  dropped in finally blocks. No new deployment or benchmark was created.
- Python test runtime: Python 3.12, PyArrow 26.0.0, ADBC manager 1.12.0,
  pytest 9.1.1. Native Go toolchain: repository-declared Go 1.26.10.
- The documented CLI example exported and re-ingested IPC file format under
  Python `-O`, with a unique table and cleanup; one row verified in each step.
- Ruff checks for changed Python source/example/tests passed. Changed Go files
  were formatted. `git diff --check` passed. Relative IPC guide links checked.

## Regression evidence

The C import-cancellation test first failed with IO instead of CANCELLED:
`failed to read ingest data: Cancelled: ...`. Existing bulk ingestion wraps
reader errors as IO. The native IPC boundary now restores cancellation/timeout
from its execution context, after the ingest path has released its reader.
The test passes with CANCELLED after the fix.

Initial empty-export integration assertions expected zero batches. The existing
query reader emits one zero-row batch, which is valid Arrow; assertions now
check that behavior while still validating schema and zero rows. The native
serializer preserves the reader's batches.

Independent PyArrow checks cover stream/file encodings in both directions;
64-bit integers above 2^53, NUL/unicode strings, binary, bool, doubles, decimals,
dates, timestamps and nulls; empty and three-batch output; native re-ingestion;
existing output preservation; script/DML rejection; truncated second-batch
input with confirmed earlier autocommitted rows; C export cancellation with
partial-file deletion and statement reuse; C import cancellation status.

## Review outcome and limits

Reviewed correctness/lifecycle, filesystem ownership and evidence against
REVIEW.md. No known actionable finding remains in this scope. No human approval,
release, cross-platform execution, new SQL streaming eligibility or fresh
memory/performance measurement is claimed. Local-file serialization adds process
filesystem access only when explicit statement options are set; destinations
must be new and inputs regular. Import remains autocommit. FIFO/network
transport, atomic publication, crash cleanup and import allocation quotas are
outside this contract. Existing historical benchmark links are labeled as such.

Broader operator streaming remains open in #227; CDC is exclusively #457.
