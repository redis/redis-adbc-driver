# Validation and measured tradeoff

2026-11-13. Agent implementation/review; human approval is pending. See
[intent](intent.md), [contract](spec.md), [plan](plan.md), the
[runner](../../../../benchmarks/table-modes/managed_read_memory.py) and
[every trial](../../../../benchmarks/table-modes/managed-read-memory-results.json).

The candidate drops consumed references from the driver-owned outer HASH reply
page. Command reply slots, fetch sizes, candidate discovery and one-page
lookahead are unchanged. This provides modest measured memory relief, with a
latency tradeoff in this run; it does not recover the pre-lookahead memory level.

## Matched native comparison

Baseline: merged main `0359c0d6b602559ff80e461a6749854ca21f1f53`.
Candidate production source: `d165099646742a69cdcf8685eace9dc4357c2b57`.
Both native libraries built with module-selected Go 1.27.3 (verified from both
libraries’ build info); binary/source fixture hashes and exact dependency
versions are in the trial file. This comparison uses the clear-only candidate;
additive command preboxing is not measured here. The runner reuses the existing
read worker and independent canonical full-value/type/multiplicity validator.

Hardware: Apple M5 Max, 36 GiB RAM, macOS 26.7.3 arm64; Docker allocated 18 CPUs
and 25,420,967,936 bytes. Redis Software 8.2.6-25 / Redis 8.8.0, Search 80808,
one RAM shard, 4 GiB database, noeviction, TLS/RESP3. Active/effective 4x QPF,
six Search workers, mnp routing and 32 connections were checked before each
trial and after completion. Started through `make -C go redis-up`; no Community
or other server substitution. No 100M ClickBench was run.

Each variant ingested the same independently typed 100,000-row ×105-column
source into an empty owned database outside timing. Each trial fetched the full
output via native C ABI into Arrow IPC. Four readers used eight disjoint ordinal
ranges; one reader used a full query. Timing includes fresh spawned processes,
imports, connections, read and IPC output; complete output validation is outside
timing. The maximum lifetime worker RSS per trial excludes the parent and is
not total application memory. The CLI argument is inherited metadata only;
this managed-only runner does not execute a CLI.

Medians of six measured trials per cell (seconds and MiB):

| Readers | Arrow batch rows | Baseline elapsed | Candidate elapsed | Change | Baseline peak RSS | Candidate peak RSS | Change |
|---|---|---|---|---|---|---|---|
| 1 | 512 | 2.203 | 2.314 | +5.05% | 270.91 | 266.11 | −1.77% |
| 4 | 512 | 1.463 | 1.471 | +0.49% | 271.66 | 262.49 | −3.37% |
| 1 | 4096 | 2.241 | 2.291 | +2.23% | 268.78 | 268.02 | −0.28% |
| 4 | 4096 | 1.416 | 1.468 | +3.69% | 276.07 | 266.88 | −3.33% |

All 56 trials passed full validation: 48 measured and eight warmups. One
invocation per binary, baseline then candidate; no timed failure, restart,
automatic retry or discarded timing. Case order alternated within each block.
Other agents paused heavy host work for the exclusive measurement window.
Baseline-first order, short trial durations, process/allocator RSS floors and
six repeats limit inference. These are observed differences, not a statistical
guarantee of performance or evidence of unchanged latency. Historical pre-#408
results used a separate run; do not combine them into a new speedup claim.

## Correctness and lifecycle

- Baseline code fails the new six-case ordinary/snapshot × complete/error/cancel
  unit regression: consumed outer reply references remain live. Candidate
  passes and leaves each original command row slice untouched.
- Focused HASH projection/prefetch/Arrow ownership race tests pass without
  external Redis (package 2.636s).
- Owned Software race run of `TestManagedHashReplyReleaseArrowOwnership` passes
  all six leaves (seven pass events including parent), no skips/failures.
  Complete directory and candidate-discovery reads check all 9001×105 values,
  exact BIGINT above 2^53, integer/string NULLs, empty and NUL/Unicode strings,
  uniqueness and completeness. A retained Arrow batch remains valid after
  Next, completion/Release and statement cancel; teardown joins lookahead.
- The real native export path was inspected: custom `exportRecordReader`
  retains once, each caller defers its original reader Release and stream
  release drops the exported reference. No corresponding leaked producer
  reference was established from the upstream Arrow documentation mismatch.

## Review

Correctness/lifecycle: the cleared slots belong to a freshly allocated outer
page; synchronous callbacks still own the local row. No command-owned slots or
string contents are mutated. Deferred clearing covers callback failure and
cancellation. Storage directory/index/pin proofs and producer queue bounds are
unchanged. Security/ownership: no new public option, SQL construction or
application-owned Redis mutation. Evidence: all completed timings are retained
in sanitized form; operational endpoints, identifiers and raw logs stay private.
No actionable findings from this agent review; human acceptance of the measured
memory/latency tradeoff remains pending. Cleanup will be recorded before handoff.

## Additive field-name preboxing

Go-redis 9.23.0 HMGet builds fresh args and boxes every field string for every
row. The additive candidate boxes the immutable field-name suffix once per
fetch batch, copies those interface values into each command’s fresh args and
queues a fresh NewSliceCmd through Pipeline.Process. The existing Exec/error
handling remains. There is no shared mutable args slice, command or reply, and
no fetch-size/protocol change. Full command equality against HMGet checks the
private first-key/step metadata defaults as well as Args/Name/FullName.

On the same exclusive host, module-selected Go 1.27.3, the
[construction benchmark](../../../../benchmarks/table-modes/managed-read-command-construction.txt)
measured five trials per implementation, one completed invocation, zero failed
or discarded trials. Per operation: 256 commands ×106 fields, including field
preboxing once per operation, fresh pipeline/commands/args and result-pointer
slice. No server, wire serialization, parsing or Arrow work is measured.

| Median | Original HMGet | Preboxed | Change |
|---|---|---|---|
| ns/op | 287,182 | 77,891 | −72.88% |
| B/op | 941,520 | 510,832 | −45.74% |
| allocs/op | 27,918 | 889 | −96.82% |

Command: from `go/`, unset Redis endpoint variables, then
`go test -run '^$' -bench '^BenchmarkHashFetchCommandConstruction$' -benchmem -benchtime=1s -count=5 .`.
Original-first order and construction-only scope limit inference; no native
elapsed/RSS improvement is claimed for preboxing until a matched native run.

Final additive correctness checks:

- Focused constructor/raw-wire/HASH/Arrow/prefetch race selection: pass, package
  2.698s. The raw RESP peer checks exact NUL/Unicode keys, duplicate/empty/binary
  fields and reply ordering, plus server errors; the pipeline hook error is
  preserved. Source fields and another command’s args/results remain unchanged
  when one command is mutated. This peer is a protocol fixture, not a substitute
  Redis deployment or performance test.
- Reused owned Software fixture through `make -C go redis-up`, verifying active
  effective 4x QPF before the live race selection. Final retained-batch test and
  adjacent directory-tail/old-pin, candidate filtering, corrupted native state,
  index proof/readiness and both SQL API projection cases pass: 29 test pass
  events, no skips/failures, package 30.88s. No timed work overlapped the checks.
- `gofmt`, Python runner compilation and `git diff --check` pass. Native library
  rebuild and matched combined-candidate comparison remain pending.
