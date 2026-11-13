# Validation and evidence

Date: November 13, 2026. Baseline `ba855f79`, candidate `699cfebf`.
All 56 planned native trials completed and passed complete independent validation.

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
used for performance claims. The separate clean run below followed native timing
with no other heavy work active.

## Native comparison

The committed `unmanaged_projection_read.py` reused the independently typed
100,000×105 source, read workers, owner/QPF gates and complete IPC/value/type/
multiplicity validator. Each baseline/candidate cell has one warmup plus six
measured trials. All 56 trials validated, with zero failures, automatic retries
or discarded native attempts. Untimed complete SQL/raw checks preceded each
block; full raw checks, final QPF gates and owned app/index cleanup passed after.
The baseline's archived library/CLI hashes and Go1.27.3 build information were
verified, with runtime GetInfo checked for both variants.

Median complete-read seconds and maximum individual worker RSS (MiB):

| Readers | Batch ceiling | Strategy | Baseline seconds | Candidate seconds | Baseline RSS | Candidate RSS |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| 1 | 512 | full LOAD* | 6.250 | 5.270 | 339.0 | 256.5 |
| 1 | 4096 | full LOAD* | 5.878 | 5.134 | 444.2 | 263.7 |
| 4 | 512 | live residual/HGETALL | 2.206 | 2.216 | 482.6 | 486.7 |
| 4 | 4096 | live residual/HGETALL | 2.093 | 2.173 | 492.7 | 495.6 |

The target one-reader full scans took 15.7%/12.7% less median time and
24.3%/40.6% less maximum-worker RSS. Four-reader filtered controls retain the
existing residual/HGETALL path; their candidate medians were 0.5%/3.8% slower,
with no improvement claimed. This does not measure all unmanaged queries.

The machine was an Apple M5 Max, 18 logical CPUs/36GiB, macOS26.7.3 arm64.
The dedicated Software8.2.6 fixture used Redis8.8.0/Search80808, one 4GiB
shard, four CPU/eight GiB container limits and the verified settings above.
Python3.15.2/PyArrow26.0.1/ADBC manager1.13.0/redis8.1.0/hiredis3.5.0
ran both variants. Baseline completed first, then candidate on the same idle
fixture; case order alternated within blocks. These blocks were not
interleaved, so time/order effects remain. Timing includes worker startup and
complete Arrow IPC writes; subsequent independent validation is untimed. RSS
is maximum individual worker `ru_maxrss`, not total process-tree peak.

The clean server-free conversion benchmark (`GOMAXPROCS=4`, three repetitions,
200ms per sample) validated every value/type before and after timing. For a
256×105 batch, median generic/direct conversion was 5.075/2.386ms,
10,035,438/658,241 bytes and 30,050/1,487 allocations. This isolates conversion;
it is not server throughput evidence.

[All native samples, source/library/CLI/harness hashes, settings and clean
microbenchmark samples](benchmark.json) are checked in. Earlier failed test
assertions and the excluded overlapping microbenchmark remain separate private
logs. No 100M ClickBench rerun or alternate server was used.

## Final broader checks and cleanup

After native timing ended, sequential unit-only `go test -count=1 -p 1 -json
./...` with both Redis endpoint variables cleared passed: 1,397 test/subtest
pass events and 1,273 expected integration/opt-in skips, seven package passes
and three no-test package skips, zero failures. This does not infer server
compatibility from skipped integration cases.

A fresh API/TLS/RESP3 gate preceded `go test -count=1 -race -p 1 -timeout=10m
-run '^TestUnmanaged' -json .` on the same owned Software fixture. All 100 pass
events completed without failure/race. The single skip was the explicitly
opt-in `TestUnmanagedPerformanceSample`; validated native timing above covers
this change separately. `GOMAXPROCS=4` applied to both final checks.

The owned Compose container/network were removed; immediate before/after
inventory confirmed every other container/network remained. Generated test.env,
data TLS assets and generated credentials were removed/redacted. Sanitized
[counts, commands, cleanup proof and private log hashes](final-checks.json) are
checked in. Raw operational logs and infrastructure IDs remain private.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
