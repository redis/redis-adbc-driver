# Validation and decision

Date: 2026-11-03 (America/Los_Angeles). Author/reviewer: Codex (agent self-review,
not a separate maintainer approval). Requestor: Albert Wong.
Baseline: `36aa28d`; final harness: `45971c8`. Issue
[#296](https://github.com/redis/redis-adbc-driver/issues/296), PR
[#298](https://github.com/redis/redis-adbc-driver/pull/298).
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and the
[complete native benchmark matrix](../../../../benchmarks/integer-simd/native-summary.json).

## Decision

No-go for integrating decoded-map batching into CPU SQL. Native resident kernels
are faster, but conversion plus native loses in 36/36 amd64 cells and 35/36
arm64 cells; the one small arm64 gain is 2.2%. The prototype stays isolated and
no release SIMD flags, SQL planner choices, CUDA ABI or exact reducers change.
See the [measurement report](../../../../benchmarks/integer-simd/README.md) for
hardware, source provenance, trial counts, allocations and representative costs.

## Local Software profile

Hardware: Apple M5 Max, native darwin/arm64 client; Docker linux/aarch64 server.
Go 1.27.3; Redis Software image `redislabs/redis:8.2.0-78.18`; Redis 8.6.2,
Search 8.6.11 (version read from the owned management API because the data proxy
omits module version metadata). Three primary shards, authenticated TLS,
verified RESP3 driver path, active 4x QPF, six Search workers, `mnp` routing,
32 connections, noeviction. `make -C go redis-up` verified active configuration
before integration checks. Client/server share one machine; this is a synthetic
fixture, not full-scale ClickBench or a remote-network performance claim.

Final profile at `45971c8`: 100,000 rows, NULL every seventh row, positive and
negative integers beyond 2^53. Each selected row, duplicate multiplicity, order
and physical int64 type matched an independent generator. Three complete SQL
execute/fetch trials passed. Timings include profiling overhead:

| Phase | Wall milliseconds | Allocated bytes | Allocations |
|---|---:|---:|---:|
| Fetch, including network/Redis/TLS/protocol/raw maps | 196.672 | 77,728,072 | 1,473,267 |
| Decode typed row maps | 26.963 | 73,616,832 | 500,024 |
| Construct typed batch | 1.729 | 816,488 | 5 |
| Scalar predicate kernel | 0.151 | 13,672 | 4 |
| NEON predicate kernel | 0.085 | 13,672 | 4 |
| Existing residual expression evaluation | 13.324 | 760,536 | 89,351 |

pprof/trace labeling adds three small allocations per phase. Kernel benchmarks
without that instrumentation are the kernel cost evidence. SQL execute through
full fetch (including harness formatting and validation) took 505.990, 511.317
and 514.884 ms. It uses `WHERE x+0>=0` to retain a CPU residual. No candidate
SQL backend exists, so these are validated baseline timings, not a speedup.

CPU sampling separates labeled fetch (70 ms), typed-map decoding (20 ms),
residual evaluation (20 ms) and the three complete SQL calls (390 ms). At a
10 ms sample period, short conversion/kernel phases have no reliable CPU sample
estimate; use their microbenchmarks instead. Allocation profiles locate map and
protocol allocation costs. The runtime network profile records 1,370.56 ms of
aggregate goroutine socket blocking over the whole test, including setup and
cleanup: 1,255.12 ms read and 114.01 ms write. Concurrent goroutine delays are
not additive wall time, and client tracing cannot separate server execution
from wire delay. Raw CPU/allocation/network traces and diagnostics stay private.

## Checks

- Full unit-only suite: `env -u REDIS_URI go test -count=1 -p 1 ./...`.
  Passed locally; Redis integration and opt-in profile skips are expected.
- Native NEON/scalar parity against an independent boolean oracle: all six
  comparisons, int64 extremes, values beyond 2^53, 0/1/50/100% NULL inputs,
  empty/odd lengths, bitmap tails and 65,536-row boundaries. Passed.
- `CGO_ENABLED=0`, `-tags purego`, and race checks for `internal/intpredicate`.
  Passed locally and on both native CI architectures.
- Cancellation before execution and between blocks, invalid shapes/operators,
  immutable inputs and independently owned outputs. Passed locally.
- Existing `TestSQLAggregateExpressionInputs`,
  `TestAggregateIntegerBoundariesAndErrorOrder`, integer DAG/fallback checks,
  CUDA CPU-side cancellation/options checks, Arrow byte-budget, required
  streaming and page-deadline tests. Passed against the verified Software fixture;
  no cases in this selected regression run skipped.
- Native amd64 SSE4.2 and arm64 NEON tests and three-trial kernel/conversion
  matrices passed in [run 41784862317](https://github.com/redis/redis-adbc-driver/actions/runs/41784862317).
  The final workflow also asserts the chosen feature and explicitly disables
  SSE4.2 to exercise runtime fallback. Final repository integration/check status
  is tracked on the [PR](https://github.com/redis/redis-adbc-driver/pull/298/checks).
- `git diff --check`, gofmt, complete matrix/trial validation and benchmark
  summary regeneration passed.

Real CUDA hardware checks were not run: this change does not modify CUDA or
claim CUDA validation. No unsupported-platform Redis tests or full-scale
ClickBench runs were performed. Non-amd64/arm64 targets use the scalar source
fallback; they were not physically benchmarked.

## Review

Inspected C bounds/tails, signed comparisons without overflowing subtraction,
NULL masking, feature gates, borrowed-pointer lifetime, output ownership,
cancellation and portable build tags. SQL execution has no production imports
of the experiment; existing lazy error/overflow behavior and floating/decimal
reductions remain in use. No operational material is included in public results.
No actionable correctness finding remains from this self-review. Repository
main had no branch protection at inspection; that is not a claim of maintainer
approval. The user requested implementation through completion and issue closure.
