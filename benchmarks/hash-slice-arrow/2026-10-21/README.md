# Direct HASH values into Arrow

Measured 2026-10-21. Follow-up to merged [#419](https://github.com/alberttwong/redis-adbc-driver/pull/419)
and [#411](https://github.com/alberttwong/redis-adbc-driver/issues/411).
Screen/benchmark baseline: exact `a937c55b` (#419 head); its Go production source
is identical to current main `a90c6b4d` after #420's test/harness changes.
Candidate initially built from `0727c7b`; rebased source is `c4a819b` with identical
production-file hashes. See [manifest](manifest.json) and
[change records](../../../docs/sdlc/changes/hash-slice-arrow/intent.md).

## Result and scope

Eligible wide simple Arrow projections now decode HMGET's owned positional values
through prebound field slots into the existing reusable Value scratch row and
Arrow builders. They skip the per-row raw string map and repeated field-name
lookups. Generic/expression/narrow/unsupported ownership paths retain maps;
rows requiring missing-default semantics reuse the existing raw-map default
calculation. This is not zero-copy: go-redis reply arrays/strings and typed Arrow
construction still allocate.

The existing #419 guard, Search discovery, query order, fields/null markers,
4,096-row cursor/pipeline bound, Arrow batch setting and RESP3 default remain.
The generic and positional paths share unchanged HMGET queuing/execution to
preserve routing, retries, cancellation and error handling. No keys are guessed,
no additional Redis commands are introduced, and snapshot isolation is unchanged.

## Five-round wide confirmation

100,000 rows ×105 columns, complete typed Arrow IPC. One warm-up and five
measured interleaved trials per method/API; reverse method order on alternating
rounds. Regular is one reader. Partition is four independent readers and eight
public ExecutePartitions/ReadPartition descriptors.

| API | Merged #419 median seconds (range) | Candidate median seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 2.772 (2.762–2.806) | 2.502 (2.482–2.556) | 9.7% |
| partition | 1.859 (1.776–1.915) | 1.666 (1.606–1.733) | 10.4% |


Both elapsed ranges are separated. Timing includes fresh spawned workers/imports,
connections, planning, complete reads, safe Arrow normalization, IPC writes and
teardown. Every value and multiplicity is checked independently outside timing.

| Metric, median | Regular baseline → candidate | Partition baseline → candidate |
|---|---:|---:|
| Client CPU seconds | 3.050 → 2.548 | 4.286 → 3.759 |
| Redis CPU seconds | 0.980 → 0.973 | 0.950 → 0.933 |
| First batch seconds | 1.871 → 1.691 | 0.974 → 0.902 |
| Max worker peak RSS MiB | 307.578 → 270.484 | 219.609 → 176.172 |
| Redis input MB | 192.892 → 192.892 | 192.906 → 192.906 |
| Redis output MB | 151.284 → 151.284 | 152.032 → 152.032 |

Memory is lower here (about 308 →270 MiB regular, 220 →176 MiB partition), unlike
#419's batching tradeoff. RSS is each trial's maximum worker lifetime high-water
mark, including imports, not aggregate concurrent memory or a byte bound.
Client CPU sums parent/reaped-worker user/system CPU; Redis counters include
small control overhead. CPU work overlaps wall time; no clean server/socket-wait
wall-time decomposition is claimed. Redis command counts and request/reply volume
are effectively unchanged, consistent with the client-side scope.

## Tuned official native references

All paths explicitly use RESP3 and identical typed Arrow IPC. Native HMGET
pipelines and Search pages are 4,096 rows, matching the driver's retrieval bound.

| Path | One reader | Four readers |
|---|---:|---:|
| ADBC #419 | 2.772 s | 1.859 s |
| ADBC positional candidate | 2.502 s | 1.666 s |
| redis-py+hiredis known keys | 2.926 s | 1.426 s |
| redis-py+hiredis Search discovery + HMGET | 3.129 s | 1.464 s |

The driver remains faster in this single-reader comparison, but **native
four-reader paths remain faster**. Native references use known schema and an
immutable, non-null fixture. Known-key reads omit Search discovery; native ranges
are explicit dense IDs rather than ADBC descriptors, and omit generic SQL,
catalog and readiness semantics. They do not establish interchangeable API
performance. Native Arrow batches contain up to 4,096 rows; ADBC still defaults
to 65,536, so first-batch latencies are not equal-size comparisons.

## Narrow control and investigated variation

The initial five-round three-column control (`watchid, userid, searchphrase`)
showed effectively flat regular time but a higher candidate partition median:

| API | Merged #419 median seconds (range) | Candidate median seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 2.499 (2.479–2.530) | 2.482 (2.409–2.536) | 0.7% |
| partition | 0.905 (0.875–0.931) | 0.981 (0.943–0.998) | -8.4% |


Retain that observation. A six-round repeat balances method order exactly:

| API | Merged #419 median seconds (range) | Candidate median seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 2.517 (2.491–2.543) | 2.516 (2.430–2.555) | 0.0% |
| partition | 0.926 (0.823–1.006) | 0.917 (0.867–1.005) | 0.9% |


The apparent narrow partition slowdown did not repeat; both regular and
partition ranges overlap in the balanced repeat. No narrow speedup is claimed.
Narrow execution retains its retrieval/decoder path; the shared append/pipeline
refactoring is covered by the full suites and these controls.

## Isolated screens, including unsuccessful alternatives

Three measured rounds after warm-up for each variant, both public APIs:

| Variant | Regular baseline → candidate | Partition baseline → candidate |
|---|---:|---:|
| Prebox field names once, independent command arrays | 2.765 → 2.822 s | 1.835 → 1.814 s |
| Preboxed fields plus bounded argument arena | 2.735 → 2.737 s | 1.809 → 1.735 s |
| Direct positional decode prototype | 2.746 → 2.551 s | 1.886 → 1.766 s |
| Positional decode plus argument arena | 2.801 → 2.602 s | 1.821 → 1.748 s |

Argument changes alone did not demonstrate a consistent overall elapsed win.
Combining the arena reduced client CPU further, but did not establish additional
elapsed benefit over the smaller positional path in these separate screens.
Only positional decoding was promoted. Do not equate allocation/CPU reductions
with measured elapsed improvements. The positional prototype had a slow regular
trial; raw trials are retained. The independently confirmed production candidate
also shares queue handling and checks bindings/reply width, unlike the isolated
prototype.

[Experiment patches](experiments/) reproduce all four isolated variants from
`git archive a937c55b`, using `git apply` followed by the release build command.
These patches are diagnostics, not supported driver options.

## Allocation diagnostics

Three complete ordinary scans of the same fixture, outside all timed matrices.
The diagnostic uses Go's Arrow allocator; primary native timings use the C
allocator. This is not a substitute for repeated end-to-end timing.

| Metric, three scans | #419 | Candidate |
|---|---:|---:|
| elapsed | 7.588328333s | 6.813201375s |
| allocated_bytes | 4925298616 | 3444522376 |
| allocations | 99348418 | 98148460 |
| GCs | 108 | 96 |

Cumulative allocated bytes fall **30.1%** (4.925 →3.445 GB); allocation count falls
about 1.2%. The removed raw maps are large allocations. The baseline raw-map
allocation site disappears from the candidate's sampled allocation profile.
Command construction (~1,000 MiB across three scans), reply arrays/strings and
Arrow buffers remain. Earlier preboxed/arena screens reduced command cost but
were not promoted without a clear additional elapsed win.

[Profile summary](profile-summary.json), [baseline allocations](base-alloc.pprof),
[candidate allocations](candidate-alloc.pprof), [baseline CPU](base-cpu.pprof),
[candidate CPU](candidate-cpu.pprof) and corresponding top tables/logs are retained.

## Verification and reproduction

Final current-main Go suite: **1,015 passing test/subtest events, 15 existing
skips, zero failures**, three packages. Focused Cluster checks pass under the
race detector for RESP3 and RESP2. Native ADBC validation on standalone RESP3,
standalone RESP2 and Cluster: **372 passed, 8 existing skips, 3 subtests passed
per run**. The exact binary also passes current main's independent restricted
TLS/ACL/resharding harness. TLS throughput was not benchmarked.

Differential tests compare positional decode with the legacy map decoder for
exact integers, NUL strings, missing/default/null-marker semantics, timestamps,
alias/duplicate outputs, malformed values and unselected required columns.
Independent wide integration tests validate all values over sparse/deleted IDs,
all-NULL rows, implicit order, empty results, LIMIT/OFFSET, active positional
update/delete-after-discovery behavior, callback lifetime, cancellation/release
and retained Arrow strings. Generic fallback is preserved.

All **164 completed benchmark executions** pass exact value/multiplicity
validation: 32 warm-ups and 132 measured trials. Raw results:

- [Primary wide/native comparison](confirm-wide.json).
- [Initial narrow control](confirm-narrow.json), [balanced narrow repeat](narrow-balanced.json).
- [Preboxed screen](screen-boxed.json), [arena screen](screen-arena.json),
  [positional screen](screen-slices.json), [combined screen](screen-slices-arena.json).
- [Final Go log](go-tests-main420.jsonl), native JUnit/logs, race logs and TLS report.

Use a dedicated Redis 8.6.2 / Search 8.6.0 server. The measured Docker server has
four CPUs/two GiB, localhost TCP without TLS; clients use macOS arm64, 18 logical
CPUs/36 GiB. No task correctness/profiling ran during timed matrices. Source,
binary, runner and fixture hashes and package versions are pinned in the
manifest/raw JSON. Loading and exact result comparison are outside timing.
The input is the independently converted official ClickBench 100K prefix,
79,578,019 logical Arrow bytes in canonical validation form, not RESP/IPC size.
See the [fixture/build instructions](../../wide-transfer/README.md).

```sh
python benchmarks/wide-transfer/compare.py \
  --baseline /absolute/base.dylib --candidate /absolute/candidate.dylib \
  --uri redis://127.0.0.1:18121/0 --protocol 3 \
  --parquet /absolute/hits-prefix.parquet --clickbench /absolute/ClickBench \
  --work /tmp/hash-slice-wide --load --repeats 5 --native-references \
  --native-pipeline 4096 --native-page 4096
```

`--load` replaces transferbench.hits; keep correctness fixtures on separate
servers. For narrow controls omit `--load` and select the three columns, using
new directories and five/six repeats. This prefix result does not establish
100-million-row transfer time, full-scale ClickBench SQL speed, a universal
client/deployment ranking or speed for generic/missing-default-heavy workloads.
CI and human review remain pending; no human approval is inferred.
