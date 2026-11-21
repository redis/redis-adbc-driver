# Bounded compaction validation

Date: 2026-11-20. Base: `1b5918e6`; candidate source is this change, varying only
`snapshotCompactionCopyBatchesPerStep` across 1/2/4 in isolated test binaries.
These are controlled candidates, not three released driver versions or an exact
unmodified-main baseline. [Intent](intent.md) · [Specification](spec.md) · [Plan](plan.md).

## Environment and boundaries

Redis Software (database reported Redis 8.8.0), one shard, verified active 4x QPF, Search workers 6,
routing `mnp`, 32 worker connections, noeviction, TLS/RESP3. RAM and Flex were
separate owned disposable fixtures set up with `make -C go redis-up`; management
verification was coordinated before testing. Infrastructure identities, endpoint
credentials and raw operational artifacts remain private.

Host: Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3 arm64. The test client
was an arm64 Linux Go test binary inside the Software container's Linux network
namespace, over localhost TLS. Timings do not use macOS Docker published ports.
Go toolchain 1.27.4, CGO disabled for Linux test binaries; no race instrumentation.
Other native/profile work was paused during these measurements. The Docker VM
and host are shared development resources, not dedicated benchmark hardware.

## Correctness

`-test.count=1 -test.run=^TestSnapshotCompaction`:

| Fixture | Result | Top-level passed | Opt-in profile skips | Test process seconds |
| --- | --- | --- | --- | --- |
| Software RAM / QPF4 | PASS | 42 | 5 | 26.073 |
| Software Flex / QPF4 | PASS | 42 | 5 | 52.339 |

Both runs cover the new bounded burst, successful lost-reply reconciliation,
lost reply followed by actual lease takeover (no writes under the successor's
token), DROP between copies, cancellation, deleted prefixes and final EOF/index
transition. Existing suites cover atomic preflight/repair, missing indexes,
source/prefix/lease fences, pinned readers, DDL, tail grafting, byte bounds,
selective retention and queue/admission behavior. These are focused compaction
suites, not the entire native driver or all canonical ClickBench queries.

An initial RAM run failed only the existing cache test's one-batch-per-step
assumption: its 257x4 source records cross the 1024-row threshold. The corrected
cache test explicitly drives one atomic batch per cold/hot/flushed cache trial;
it retains exact digest/body-count assertions. The full suite then passed.
The takeover/tail test now expects up to four atomic batches in a step and still
verifies lease expiry, old-reader retention and newer-tail installation.

## Isolated 105-column Flex control

Explicit opt-in `TestSnapshotCompactionWideProfile`: 4096 logical rows across four
1024-row disjoint append segments, 105 NOINDEX public columns (52 string columns
with NULLs, 53 exact BIGINT columns including ID). All 4096x105 cells are fetched
and independently checked after completed compaction. No caller replay. This
is synthetic wide data, not the canonical ClickBench schema/value distribution.
There are no concurrent writers during the timed interval and no pacing delay.
Setup and complete final fetch/validation are outside the maintenance timer.

Three repetitions per candidate, orders 1/2/4, 4/2/1, 1/2/4:

| Atomic batches per step | Max ordinary copied rows per step | Median maintenance ms | Range ms | Copy steps | Atomic copy commits |
| --- | --- | --- | --- | --- | --- |
| 1 | 64 | 635.201 | 631.071–665.489 | 65 | 64 |
| 2 | 128 | 589.850 | 588.589–605.479 | 33 | 64 |
| 4 | 256 | 556.277 | 551.983–579.961 | 17 | 64 |

Copy-phase step time medians: 615.162 / 574.899 / 544.788 ms. Index-phase medians:
2.308 / 2.447 / 2.420 ms; install-phase medians: 4.221 / 5.176 / 4.419 ms.
Total includes phase observations outside step timers, so these do not sum exactly.
There is one final empty-source copy step in every candidate. Catalog acquisitions
and releases in the copy phase decrease 65→33→17, while all candidates perform
64 bounded atomic copy commits. Additional root reads protect burst boundaries.
GC/control work remains included in its containing step, not separately isolated.

The four-batch candidate's median maintenance interval is about 12.4% shorter
than the one-batch control here. This supports reduced repeated control work for
this fixture. It is not a 12.4% ingestion speedup, an SSD cold-data result, a
causal explanation of the historical ~105-row/s observation under concurrent
index sealing, or evidence of distributed/core scaling.

## Limits and remaining acceptance

No per-command-thread/Search/client CPU attribution, phase-separated storage I/O,
maximum resident memory, cold SSD working set, concurrent four-Arrow-writer load,
full-scale backlog/cleanup lag or sustained admission experiment was collected.
Unsupported diagnostics were not guessed; these measurements remain unperformed.
Existing isolated-suite checks are not a new full-scale backup/restore proof.
The burst shares the existing two-second maintenance context; cancellation and
atomic bounds are tested, but sustained fairness under production contention
needs the remaining wide workload. Keep #565 open until its broader acceptance
has independent source-specific evidence. No operational ingestion or AWS job
was restarted, and no full-scale data was loaded for these checks.

Product-version wording is clarified in the [measurement provenance erratum](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/provenance-erratum.md); historical timings and settings are unchanged.
