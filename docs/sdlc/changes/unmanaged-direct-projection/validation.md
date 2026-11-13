# Validation and evidence

Date: November 13, 2026. Baseline `ba855f79`. Native timing remains pending;
no throughput or RSS improvement is claimed yet.

## Regression and correctness

- A Go overlay replaced only `scan_projection.go` with the actual baseline
  version while compiling the new tests. The baseline compiled, then both narrow
  and wide unmanaged selection cases failed because projection was absent.
  The complete expected-failure log is retained privately.
- Focused candidate `go test -count=1 -race -p 1` selection passed: 26 pass events,
  two endpoint-gated live-test skips, zero failures/races. Coverage compares
  generic decode/evaluation against direct projection for exact/binary keys,
  aliases/duplicates, empty/missing fields, native and text encodings, overflow
  and malformed smallint/bool/date/time values, required-input/error ordering,
  invalid namespaces, cancellation and retained Arrow buffers.
- Unmanaged columns retain `columnMeta.decode`; managed-only bound-decoder
  shortcuts are deliberately not applied to mutable external values. Existing
  LOAD*/HGETALL retrieval, predicate residuals and fetch/page limits are unchanged.
- Owned Redis Software was created through `make -C go redis-up`, then configured
  as one 4-GiB RAM shard with four CPU/eight-GiB container limits. Active/effective
  4x QPF, six Search workers, mnp routing/32 connections, noeviction and verified
  data TLS/RESP3 were checked before live tests. Management uses its fixture
  bootstrap self-signed certificate; data TLS is certificate-verified.
- Final live race selection passed: 12 pass events, zero skips/failures/races.
  A 9001×105 corpus validates every value, NULL, empty/NUL/Unicode string, exact
  integer above 2^53 and row multiplicity. Retained records survive Next, complete
  or early Release and cancel. Additional tests cover unfiltered __key output,
  binary source keys, point lookup output, live source-predicate fallback,
  changed/deleted/wrong-type candidates and index changes. Reads and DROP preserve
  source HASHes, arbitrary application __rowid fields and application indexes.

Initial new-test assertions incorrectly expected a streaming projection for
materialized small results/point-key lookup. The final test uses seven-row
batches to inspect full/range streams and validates point-key output through its
existing materialized fallback. One diagnostic initially accessed its reader
after Release; that test-only diagnostic was corrected. These failed attempts
are retained separately; production code did not change for them.

One conversion microbenchmark invocation overlapped this task's live correctness
work. Its output remains a separately labeled diagnostic, and its timings are not
used for performance claims. A clean measurement is pending the parent slot.

## Native comparison plan

The new `unmanaged_projection_read.py` reuses the existing independently typed
source, read workers, owner/QPF gates and complete IPC/value/type/multiplicity
validator. Compare baseline and candidate one/four-reader, 512/4096-row ceilings,
six measured repetitions after warmup per cell. Full scans select the new direct
projection; filtered range statements retain the live residual/HGETALL control.
All attempts and source/binary hashes must remain visible, with no SQL replay.

Raw logs, endpoint credentials, operational artifacts and infrastructure IDs
remain private. Fixture cleanup will follow final verification.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
