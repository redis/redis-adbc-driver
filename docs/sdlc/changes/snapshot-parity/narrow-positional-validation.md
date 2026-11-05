# Positional values for narrow snapshot projections

Date: 2026-11-05. Parent: 5a4604a, production c06e181.
Status: focused API/native checks and corrected allocation/native comparisons
pass. Full current-source ordinary/snapshot/platform checks remain required.
See [intent](intent.md), [spec](spec.md),
[plan](plan.md) and [implicit-order evidence](implicit-order-validation.md).

After exact snapshot visibility selects the winning physical HASH, a narrow
simple stream still builds an intermediate HASH map and decoded row map.
The existing positional HASH/Arrow path is limited to wide projections, even
though it already checks physical row identity, tombstones, missing values and
NULL markers. Extend that path to narrow directory/candidate reads without
changing ordinary live Search fetch eligibility or exposing a product option.

This changes eligibility after visibility resolution only. The existing simple
projection binder still rejects unsupported expressions. Residual predicates,
tie sorting, late projection, pushed limits, descending/other sort orders and
unmanaged HASHes retain their existing paths. Prefetch's separate width gate
is unchanged, so narrow streams do not acquire new prefetch workers.

The new operator-level unit test fails against c06e181 because the real snapshot
scan cannot bind positional values. After the change it checks duplicate/aliased
outputs, embedded NULs, exact BIGINT beyond 2^53, row identity, an older row's
missing-column default and its explicit NULL override. Seven boundary cases
retain map/materializing fallbacks. The focused race selection, including
existing map-versus-positional decoder comparisons, provenance, projection and
wide typed values, passes; root 1.972 s. An initial build had an unused local
after removing the wide-only eligibility check; it was corrected before this
passing run. Redis endpoints are unset for these unit checks; they do not prove
server compatibility.

The existing both-API projection integration case additionally requires
positional values on narrow shared-generation partitions, comparing every
returned value with the independent buffered path. Segmented ordinary streams
also require positional binding when the eligible path is selected.

The subsequent verified Software race selection passes 57 test events, zero
skips/failures, root 12.990 s. It includes the strengthened both-API case,
early-release/cancellation, HASH integrity, narrow boundaries, provenance,
implicit/explicit ordering, candidate visibility, prefetch, NULL-order proofs,
readiness transitions and Substrait visibility. The earlier c06e181 root snapshot
selection completes separately with 430 passes / 25 explicit skips / no failures,
964.963 s; it does not cover this later change.

Server checks are sequential on owned Redis Software 8.8.0, strict verified TLS,
active/effective 4x QPF and noeviction, three primaries behind its proxy; macOS
ARM64 / Go 1.27.3. These do not establish Cluster API or recovery compatibility.

Current production/server evidence remains attached to c06e181 until fresh
checks of this change finish. Its complete snapshot race process was already
compiled and running before these edits; do not relabel its result as coverage
of this new eligibility change. Keep matched native benchmarks isolated from
all local builds, tests and database probes, and retain both measured gains and
remaining mutable gaps. No performance gain is claimed yet for this change.

## Initial native comparison and allocation finding

At eligibility-only source 6a041e5, complete native validation passes 565 tests
and three subtests, eight explicit capability skips, 86.56 s. The versioned
library SHA-256 is
`2bde8027bdcdd9fcc937390f002705db0231d3422040cddda254e604c5165075`.

Six isolated native processes run c06e181, 6a041e5, mutable f4d5b78, then reverse
order, using the unchanged eight-case harness: 20,000 input rows,
replacement/deletion, 32 unrelated tables, one warmup and five measured samples
per case/process. Every timed sample fetches and checks all values; the complete
implicit read also checks its Arrow schema. Setup is excluded. Active/effective
4x QPF, noeviction and strict TLS are verified per process. A local process census
before each process and every 500 ms rejects a run if a Go test/build, compiler
or pytest appears; the six accepted processes have zero violations.

For the full 19,999-row implicit read, pooled median is 757.074 ms at c06e181,
780.400 ms at 6a041e5, and 144.909 ms mutable. Positional eligibility alone does
**not** establish a latency improvement. Two processes per build and ten samples
do not establish a general regression or speedup. The complete
[eight-case samples and provenance](narrow-positional-native-comparison.json)
retain this negative result. This is synthetic load, not ClickBench.

An in-process allocation benchmark exercises the real fetch/decode helpers with
identical intercepted HMGET replies, 256 rows/page and every decoded value
checked. It connects to no Redis and is not native/Software performance evidence.
Three repetitions show the present-value path dropping from 194,368 bytes /
2,833 allocations per page to 112,576 bytes / 2,321 allocations (42.08% fewer
bytes). However, missing-default rows still reconstruct a map: 198,592 bytes /
2,833 allocations on the positional path. This is why a missing-only initial
allocation probe showed no saving. The expanded benchmark retains both shapes.

The follow-up binds NULL-marker positions and shares the existing default
cutoff/type decoder through `missingValueForID`. It preserves missing row-ID
behavior and any-string NULL-marker presence, including empty markers. It avoids
constructing a map for default resolution. The follow-up unit race selection
passes (root 1.980 s); the expanded live selection, including ADD/ALTER defaults,
ingestion defaults and temporary defaults, passes 65 test events, zero skips or
failures, root 20.124 s. Final allocation/native checks of that correction remain
recorded below. No end-to-end gain is inferred from an in-process allocation test.

Native comparison Python 3.13.16 / Arrow 26.0.0 / manager 1.13.0; native suite
Python 3.14.10 / Arrow 26.0.1 / manager 1.13.0. Host Apple M5 Max, 18 logical
CPUs / 36 GiB; Docker 18 CPUs / 25,421,033,472 bytes, checked after comparison.

## Corrected default kernel and final native comparison

At 8d59eeb, the complete native suite passes 565 tests and three subtests,
eight explicit capability skips, 84.22 s. The versioned library SHA-256 is
`fe36bbaed9c2ef65edfd69620d5c5d0c2e556b8f0b78727dbc5bd06d5816d993`.

The allocation benchmark runs three repetitions of each complete 256-row page
shape. Pooled medians are shown below. These are in-process fetch/decode costs
with intercepted replies, not Redis/native latency or whole-process memory.

| Shape/path | ns/page | bytes/page | allocations/page |
|---|---:|---:|---:|
| Present / maps | 115,676 | 194,368 | 2,833 |
| Present / positional | 83,806 | 112,576 | 2,321 |
| Missing defaults / maps | 117,524 | 194,368 | 2,833 |
| Missing defaults / positional | 83,074 | 112,576 | 2,321 |

Both shapes use 42.08% fewer bytes and 18.07% fewer allocations per page.
The initial default-map result remains in the
[before/after allocation data](narrow-positional-allocation.json), with the
unchanged test-harness hash and every sample. Correct values, duplicates,
default cutoffs and NULLs are checked inside every iteration. Retain this as a
measured kernel resource reduction, not a claim that native read memory drops
by the same percentage.

A separate six-process comparison repeats c06e181, final 8d59eeb, mutable
f4d5b78, then reverse order, with the identical eight-case native harness and
parameters described above. All six processes pass complete independent value
checks and the implicit Arrow schema check; the isolation guard sees zero
violations. [All final samples and source/harness/library hashes](narrow-default-positional-native-comparison.json)
remain separate from the earlier eligibility-only comparison.

The full implicit read has pooled median **667.164 ms → 612.826 ms** (8.14%
lower / 1.089x improvement). Mutable is **122.454 ms**, leaving a **5.005x**
median gap. Final p95 is 774.340 ms versus preceding 952.155 ms. Two processes
per source and ten samples are limited evidence, not a statistical or general
workload guarantee. Other seven cases remain reported without attributing
their variation to this change. Historical native samples are not relabeled.

Author review checks immutable visibility before projection, marker presence
versus value, exact row IDs, missing/default semantics, binder failure state,
unchanged fields/ACLs, bounded buffering, cancellation and resource release.
No protocol, public option or input-replay behavior changes. Restricted-reader,
explicit-sort cap handling, Cluster/recovery, remaining write/utility/resume
and full performance-parity gates remain open. This is not maintainer approval.
