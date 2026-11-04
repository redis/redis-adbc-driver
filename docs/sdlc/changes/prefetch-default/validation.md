# Validation

Disposition: **enable prefetch by default** for eligible multi-page managed HASH
streams requiring at least 64 columns. Known single-page, narrow, GPU and
unsupported plans preserve their existing paths. `scanprefetchoff` is a build-only
synchronous opt-out. The legacy `scanprefetch` tag remains accepted.

The [complete report](../../../../benchmarks/prefetch/2026-11-03-default/README.md)
contains raw trial JSON, paired intervals, memory costs, source/build identities,
independent source proof, regressions and cleanup verification. No maintainer
approval or universal speedup is claimed.

## Source and elapsed qualification

PR #297 merged at `4735cbd`. Corrected matched runtime
`158b8c820c476a7ae116fb677c6f1fffa4722b98` includes both regression fixes below.
Off/on production trees are identical, built with Go 1.27.3 and matching flags.
The four separate matrices each contain one excluded warm-up plus twenty
alternating-order measured pairs for wide/narrow × regular/partition × one/four
readers: 336 executions each. All four passed the predeclared elapsed rule:
repeatable one-reader wide improvement and wide paired-bootstrap upper bounds
within the +5% concurrent-reader regression margin.

| Boundary / wide columns / GOMAXPROCS | One-reader regular / partition median improvement | Four-reader regular / partition median improvement |
|---|---:|---:|
| Go / 105 / 16 | 14.10% / 14.96% | 1.10% / 6.57% |
| Go / 64 / 16 | 12.28% / 13.10% | 6.20% / 3.10% |
| C/Python / 105 / 16 | 14.03% / 14.35% | 5.52% / 8.70% |
| Go / 105 / 4 | 14.04% / 14.99% | 8.53% / 3.62% |

Every one-reader wide case won all twenty pairs. Some concurrent intervals
include zero, so lower medians do not establish a repeatable gain in every case.
The four-CPU control limits the Go runtime on the same 16-vCPU host. Three-column
controls, all elapsed ranges and outliers remain in the report.

The final normal-default source
`07bf8decabfed75c9f1a93d27051f16970e0c30c` separately passed Go/C restored-fixture
confirmation, adding 192 validated executions (five measured pairs per case).
Existing production files match the primary runtime except the selectors.
Main's #298 isolated SIMD package is not linked into the driver. Final timings
are retained separately and never pooled with primary twenty-pair matrices.
Including 32 separate wire diagnostics, **1,568 executions** independently verify
all selected values, Arrow types, NULL bits, duplicate multiplicities, complete
100K row counts, requested active readers and effective 4x QPF outside timing.

## Fixture and memory

All 48 compatible snapshots passed SHA verification; 99,997,497 rows were restored
on Redis Software with active verified 4x QPF, authenticated verified TLS and
RESP3. The 100K fixture uses pinned official DDL and exact restored IDs via SQL
INSERT SELECT outside timing. Exact ID/unique WatchID membership selects the
independent official Parquet reference. Physical Arrow source and raw IPC are
archived privately with checksums. Historical Community snapshot provenance is
explicit; all restores and tests in this qualification use Software.

Ownership is bounded to the current reply page plus one additional page per
reader. Discovery requests 4,096 rows; this is not a strict byte quota. Variable
strings and concurrent readers determine actual costs. C wide child process-RSS
medians increase approximately 17–24 MB; narrow controls also show an offset
without starting prefetch, so the full RSS delta is not a page-retention measure.
Go sampled RSS medians are close. Current child RSS is sampled independently;
inherited child ru_maxrss is diagnostic only. C Go allocation/heap counters are
unavailable, not zero. A synthetic extra owned page used about 28.44 MB under its
specific 4,096 × 105 string fixture. No snapshot guarantee is added.

## Correctness and checks

Two failures were reproduced before correction:

- `TestPrefetchDetectsDropAfterProducerEOF`: table drop during final conversion
  incorrectly returned EOF. Consumer EOF now checks readiness after conversion,
  preserving the existing stale-read `adbc.StatusIO`.
- `TestPrefetchAvoidsSinglePageWorker`: a known zero-cursor initial discovery
  unnecessarily launched lookahead. It now remains synchronous. The active-worker
  guard short-circuits before reading worker-owned cursor fields.

Both regressions and focused lifecycle tests pass under the race detector.
The live narrow-projection assertion also confirms fewer than 64 needed columns
and no worker (14.255 s). No assertions or skips were weakened.

The complete **normal default build**, without the experimental tag, passed
`make -C go test-software TEST_ARGS='-race'` on the isolated task-owned verified
4x QPF TLS/RESP3 three-shard Software fixture: driver 356.591 s, 840 top-level
passes and 43 existing skips; six tested packages pass, plus the CUDA stub package
with no tests. Skips cover proxy ACL/protocol introspection limits, opt-in profiles
and unavailable native GPU/extra endpoints. The corrected experimental runtime
also passed the complete suite before timing (417.459 s, 837 passes/41 skips,
before isolated SIMD tests were merged).

The explicit opt-out unit suite passes all six packages (driver 26.351 s);
expected integration skips there are supplemented by live restored-fixture
on/off path-selection proofs. Normal/opt-out arm64 C libraries pass all eight
regular/partition × one/four-reader synthetic 5,000 × 105 cases, checking exact
values, types, NULLs, embedded NUL strings and integers above 2^53. Eight Python
validator/meter/RSS/report tests pass, including an actual child allocation and
unavailable C counters. These synthetic checks are not ClickBench timings.

A temporary monitor environment lost its dependencies; a task-owned environment
restored monitoring without affecting remote trials. The first cleanup-Lambda
configuration check observed stale state; the exact pilot configuration was
verified before expansion. No duplicate fleet was provisioned.

## Cleanup and review

Task-owned benchmark instances, security group, key pair, cleanup rule/function
and isolated local Software project are removed after verified evidence archival.
[Cleanup proof](../../../../benchmarks/prefetch/2026-11-03-default/cleanup.json)
records counts, any accepted asynchronous disk deletion and conservative compute
cost including the previous run. Existing backups and shared IAM resources are
retained. The original aggregate $300 cap and eight-hour deadline apply to both
runs. Infrastructure identifiers, endpoints, credentials and licenses remain
private. Human maintainer review is required before merging the follow-up PR.
