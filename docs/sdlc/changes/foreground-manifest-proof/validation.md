# Foreground manifest proof validation

Date: 2026-11-06. Issue [#336](https://github.com/redis/redis-adbc-driver/issues/336),
[PR #346](https://github.com/redis/redis-adbc-driver/pull/346).
See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
Production baseline: `2eeb82d93636f653f3feccec704863bd7fe9b35a`;
candidate: `cbb5b0f4aedabf254601d20a3fe3e458ae18a6c3`.
Later evidence-only changes do not alter production code.

## Change and review

Acquisition and workspace creation each read a committed work record, directory
cardinality and root-map cardinality. The three native reads now share one
pipeline instead of three sequential round trips. A valid pin protects the
immutable root, and subsequent lease checks remain. The previous serial reads
were not an atomic transaction either. No new Lua, cache, storage field, grant,
index rebuild, catalog copy or write/input retry is introduced.

Every original manifest/count bound and mismatch refusal remains. Command errors
are checked individually in the original proof order. The first candidate
ignored the pipeline's overall error after healthy individual values: a controlled
hook reproduces the false success in **0.03 s**. The corrected candidate returns
that error and passes the same regression under the race detector in **0.02 s**.
The fault changes only the pipeline outcome; it does not corrupt Redis or replay
commands. This was a candidate bug, not a claimed baseline driver defect.

Final review checks correctness/lifetime, ACL/data ownership and source/evidence
mapping. No remaining actionable finding is identified in this delta. This is an
agent review, not a formal human approval. The separate recovery investigation
[#345](https://github.com/redis/redis-adbc-driver/issues/345) remains unresolved;
this performance change is not its fix.

## Controlled foreground attribution

The opt-in `TestSnapshotForegroundFixedWriteProfile` exercises a 20,000-row point
target, zero-affected UPDATEs and one-/512-row Arrow appends with **0 and 32
unrelated empty tables**. Four samples per case retain exact int64 values above
2^53 and complete final verification, including unrelated tables. Workers are
disabled and each target remains below the eight-segment admission limit. This
attributes fixed work; it does not measure sustained compaction or native API
throughput. Three samples after the first warmup form each reported median;
command attribution averages all four samples.

| Case | Baseline round trips/op | Corrected round trips/op |
|---|---:|---:|
| Point UPDATE | 64.5 | 60.5 |
| Zero-affected UPDATE | 50.25 | 46.25 |
| One-row Arrow append | 64.5 | 60.5 |
| 512-row Arrow append | 67.0 | 63.0 |

Both catalog sizes show the same four-trip reduction, with unchanged native
command counts. Fractional averages reflect other per-operation maintenance
work. Instrumented wall times vary, including negative preliminary comparisons;
no latency claim is based on this profile alone. The
[category summary](attribution-summary.json) and lossless
[raw phases/samples](attribution.json.gz) retain baseline, preliminary candidate
and corrected candidate separately. Read the raw JSON with
`gzip -dc attribution.json.gz`; the preliminary uncommitted candidate precedes
the overall-pipeline-error correction and is not labeled as `cbb5b0f4`.

## Correctness checks

- All six executable unit packages pass with live endpoints unset; root
  **27.310 s**. Expected integration skips are not deployment proof.
- Focused Software metadata/publication/independent/no-op/Arrow selection:
  **48 pass events / no skips** on the initial candidate. This predates the
  overall-error correction; the complete final selection below supersedes it.
- Final complete root snapshot race selection: **551 pass events / 44 explicit
  skips**, no failures or race reports. It includes malformed/missing manifests,
  wrong directory/map types and counts, refusal precedence, the pipeline-only
  error, metadata sharing, publication/dependency conflicts, unknown outcomes,
  ownership and Arrow admission. Command: compiled `go test -race -c` binary,
  `-test.count=1 -test.parallel=1 -test.timeout=30m -test.v -test.run Snapshot`.
- Actual Software-principal SQL RBAC race selection: **21 pass events / no
  skips**, including restricted reads, grant lifetime and payload boundaries.
- Native snapshot C/ADBC suite: **19 tests / no skips, 10.754 s**.
- Full official native validation: **565 tests + 3 subtests passed / 8 capability
  skips, 76.24 s**. Official validation revision
  `47a5ddf3f2a617ac88da3891848e000c74d55b0e`; Python 3.14.10.

The final full race binary has the exact production/test sources committed as
`cbb5b0f4`, compiled immediately before that commit. Native libraries are built
with `make -C go build`; both embed clean VCS revisions and **Go 1.27.3**:

| Library | SHA-256 |
|---|---|
| Baseline | `41ba5d2d8be82c377c6204a1b641bcbc59a82a8ec481d023d45de825ef35b9dd` |
| Candidate | `a6bdc5babfccc2e5c6db5206637cc18477b39cde764f219023d62e3fc9d2903a` |

The native launcher initially used a relative path after the setup script changed
working directories. It stopped before collection and yielded no native result;
the corrected absolute-path invocation produced the complete results above.

All live checks use the owned **Redis Software 8.2.6-25 / database Redis 8.8.0**
fixture, strict data TLS, noeviction and verified active/effective **4x QPF**.
Its three configured primary shards share a Software node behind the proxy.
This is not new actual Cluster API, failover or persistence-recovery evidence;
those explicit full-suite skips are not relabeled as supported fault tests.
Native snapshot Python is 3.13.16; machine/toolchain details and the exact public
Software image digest are retained with the comparison.

## Matched native before/after comparison

[All runs, samples, hashes, settings and process-census results](native-comparison.json).
The same harness and fixture run each library in independent Python processes,
baseline → candidate → candidate → baseline for each workload. Small writes use
one initial row, **0/32 unrelated tables**, one warmup and seven samples per case
per process: **14 measured samples/source/case**. SQL/Arrow batch cases use 512
rows with exact int64 values and full independent verification.

Write timing includes publication and complete RETURNING fetch where present;
independent full-row verification is outside the timer. Setup is excluded.
Snapshots, native authorization and normal maintenance stay enabled. No caller
SQL/input retries occur. Every process verifies Software QPF4/noeviction and TLS
before timing. All local builds, tests and profilers finish first; a 500 ms census
plus per-process preflight detects **zero known competing build/test/profile
processes** across **103 observations**. No extra Redis probes run in timing
windows. Normal server indexing/maintenance is part of the workload; physical
runtime shard placement is not sampled during timing.

The first four batch processes show a **9.65% higher pooled SQL-insert median**,
driven by one candidate process (process medians 25.828 and 31.843 ms versus
26.183 and 25.843 ms). That result is retained. A focused eight-process repeat
in both source orders investigates the regression without changing source,
fixture or harness parameters. Including **all** original and repeat batch
samples gives six processes and **42 measured samples/source/case**. The
slowdown does not persist in the combined median, but this does not establish
its cause or a general batch speedup.

There are **20 complete processes / 392 measured operations** in total, with
complete independently calculated results for every retained sample. The table
pools all samples within each workload/source, without dropping failed tails:

| Workload | Unrelated tables | Baseline ms | Candidate ms | Latency change |
|---|---:|---:|---:|---:|
| Metadata COMMENT | 0 | 15.616 | 13.486 | -13.64% |
| Point UPDATE | 0 | 24.571 | 22.874 | -6.91% |
| Zero-affected UPDATE | 0 | 14.457 | 14.323 | -0.93% |
| One-row Arrow append | 0 | 20.675 | 20.322 | -1.71% |
| Metadata COMMENT | 32 | 14.638 | 13.629 | -6.89% |
| Point UPDATE | 32 | 24.819 | 23.081 | -7.00% |
| Zero-affected UPDATE | 32 | 14.875 | 13.748 | -7.57% |
| One-row Arrow append | 32 | 22.500 | 21.319 | -5.25% |
| 512-row SQL insert | 0 | 26.425 | 26.351 | -0.28% |
| 512-row Arrow append | 0 | 26.430 | 26.020 | -1.55% |

Negative change is lower latency. The point-UPDATE improvement is about 7% in
these bounded samples. Small Arrow gains are modest; batch differences and the
near-flat zero-affected case with no unrelated tables do not establish a stable
gain. This does not prove sustained throughput, larger-catalog or read-performance
parity. Host: Apple M5 Max, 18 logical CPUs, 36 GiB; Docker 18 CPUs /
25,420,967,936 bytes. The root shell's default Go is 1.27.2; the module's embedded
library build toolchain is 1.27.3 as recorded above.

Hosted [Test](https://github.com/redis/redis-adbc-driver/actions/runs/42273280716)
and [Release](https://github.com/redis/redis-adbc-driver/actions/runs/42273280836)
checks at `cbb5b0f4` are still running when this report is prepared. Later PR-head
status must be inspected separately. No skipped, canceled or unfinished job is
counted as a successful platform check.
