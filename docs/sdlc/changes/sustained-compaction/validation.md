# Sustained maintenance validation

Date: 2026-11-06. Issue [#337](https://github.com/redis/redis-adbc-driver/issues/337),
delivery [PR #348](https://github.com/redis/redis-adbc-driver/pull/348).
Status: unconditional experiment complete; gated refinement and delivery pending.
See [intent](intent.md), [spec](spec.md), [plan](plan.md) and [design](design.md).

## Sources and correctness

Preceding snapshots: `c6baae32d77fd13575f0fc46defaaf761f699df4`.
Unconditional experiment: `778f0a4c3301664cacfc2247c5642bef71633a7e`.
Final production candidate preserves serial reads for small jobs; fresh gated
correctness/profile/native results remain pending at this checkpoint.
Mutable performance reference: `f4d5b787e8c28c036baf49de9190c4e6f83ca2e8`.
The three initial native libraries embed clean VCS identities and Go 1.27.3, built with
the repository Makefile/version flags before matched timing. Later observer and
offered-load changes affect only the Python harness and documentation.

The following initial correctness checks cover `778f0a4c`, not the later gated
refinement. All live checks use the owned Redis Software fixture, active/effective 4x QPF,
strict TLS and noeviction. Three configured primary shards share one Software
node/proxy; managed snapshot keys use one hash slot. This is not cross-shard
snapshot or new promotion/persistence coverage. Recovery incident #345 remains
separate and unresolved.

- Focused read-pipeline/atomic/directory/index races: 16 pass events, no skips.
  The original individual-read refusal fails on unchanged production in 0.37 s.
- Complete Software snapshot race selection: 572 pass events / 58 explicit
  skips, all six executable packages pass; root package 367.509 s. No failure or
  race report. Preserved-native/Cluster/special-principal/profile skips remain
  skips, not compatibility evidence.
- Endpoint-free unit races: 1,211 pass events / 1,081 expected integration/profile
  skips; all six executable packages pass, root 52.948 s.
- Actual Software-principal SQL SELECT grant races: 22 pass events, no skips.
- Native snapshot C/ADBC: 19 tests, no skips, 10.840 s.
- Complete official native ADBC validation: 565 tests plus three subtests pass,
  eight capability skips, 75.90 s (JUnit records 568 passing cases / eight skips).

The page/cardinality checks retain their original behavior for ordinary readers.
Pipeline-level and per-command read failures in either rank or page phase leave
copy progress/output unchanged. The full snapshot selection additionally covers
pinned streams, exact visibility/tombstones, newer tails and DDL, stale-owner
takeover, unknown copy outcomes, cancellation, writer admission and reference-safe
reclamation. Existing bounds, pacing, GC and mutation protocols are unchanged.

## Measurement corrections and negative evidence

The first smoke's approximate storage probe encountered a legitimately retired
index between FT._LIST and FT.INFO. The harness handles the existing typed
missing-index response only in that statistics probe; query errors and driver
assertions are unchanged. Its failed numeric record is retained.

Initial measurements sampled backlog after full-row validation and started the
drain clock after the final storage probe. These gave maintenance extra time
before observation. The corrected harness takes an atomic, read-only bounded
backlog sample immediately after each timed window, then validates all rows and
samples storage. Drain elapsed time begins at the final completed write and
includes validation/probing. The canceled initial series and earlier exploratory
samples remain separate with their original harness hashes. They are not
relabeled as corrected observations or successful complete processes.

An unpaced corrected-observer attempt completes its snapshot processes at
28.84 preceding / 27.37 candidate writes/s: an observed negative candidate case,
not evidence of a general sparse-write gain. Its mutable process fails in the
third window with a connection reset while reading table metadata, after two
fully validated windows. A separate clean mutable reproduction fails the same
way. No caller retries are used. Incomplete processes are not successful native
comparisons, and their failed-window throughput is not retained as a pass.

After the failures, the fixture remains active with effective QPF4/noeviction,
zero container restarts/OOM kills and zero rejected connections. This health
sample does not identify a cause. The resets are not declared fixed, random or
a proven Redis defect. A separate 60-second mutable observation with an explicit
40-start/s ceiling completes all windows and row checks. The final matched
comparison uses that same ceiling for every source and records actual pacing
and delivered rates; it does not measure mutable peak throughput.

## Timing and storage interpretation

Every timed window includes complete SQL/publication and complete RETURNING
fetch. Every returned ID/value is independently checked, including BIGINT above
2^53. Full final row/value/NULL/multiplicity checks run between windows and after
the drain observation. No SQL/input retry, manual compaction, sweep or forced
foreground copy is used.

The workload is windowed sustained offered load, not an uninterrupted soak.
Between-window full fetches and storage scans create explicit quiet gaps. Their
durations and immediate end-of-load backlog are retained, so later low backlog
is not substituted for pending work at load stop. The process census checks
known local compiler/test/profile processes every 500 ms; it does not prove the
whole host has no unrelated activity. Only the owned Software container runs.

Native key memory is the deduplicated SCAN/MEMORY USAGE sum while maintenance
continues; deleted keys can disappear during observation. Search memory fields
sum native FT.INFO fields across observed indexes; fields overlap and must not
be added together into a new total. These are approximate native allocations,
not RSS, allocator accounting, network bytes or synchronized retained snapshots.
An index count is the FT._LIST observation; retirement during INFO is allowed.

Drain completion means no eligible driver metadata/data queues, compaction job
or candidate and only the current manifest. Mutable storage has no such driver
maintenance and its drain comparison is not applicable. Neither case proves all
server-internal Search GC or allocator memory has returned to the OS. First
observed empty state is an upper bound at the reported cadence, including final
validation/probing time, not an exact instant of physical memory release.

Final tables, raw numeric evidence, review, cleanup and terminal CI will be
recorded after the matched series finishes. No full feature/performance parity
or stable general write speedup follows from the controlled profile alone.
