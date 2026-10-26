# Wide Search discovery and HASH fetch: #411

Measured 2026-10-21. Baseline: merged #416, `4b0fdb5ce2a1a29651bad778aa58e4198ead0e70`.
Production candidate: `dbdafa2f1115fc6ff91eac9ad8c7d50cb6a8b357`.
[Issue #189](https://github.com/redis/redis-adbc-driver/issues/189),
[change records](../../../docs/sdlc/changes/search-hmget-speed/intent.md),
[manifest](manifest.json). RESP3 remains the default; every comparison here
explicitly requests RESP3 on both driver and native clients.

## Result and implementation

The earlier HMGET experiment reduced bytes but was slower. On current main,
**direct adoption of fetched maps plus 4,096-row cursor/pipeline batches improves
both public APIs** in repeated local wide scans. The production path is guarded:
at least 64 needed columns, more than half the table, an existing LOAD-all plan,
managed row-ID ownership, no residual predicate, late materialization, tie sort
or pushed limit, and either no order or ascending internal row-ID order.
Other scans retain their retrieval strategy and 1,000-command pipeline bound.

Search discovers actual keys and row IDs; it never guesses keys. Bounded HMGET
returns authoritative values including existence/null-marker fields. The fetched
map becomes the raw row directly, carrying over only the small cursor metadata.
This avoids copying every field into a second map. Server ordering, exact decode,
Arrow construction, readiness/generation checks and cursor/worker cleanup remain.
There is still a two-stage read window: a row can change or disappear after
Search discovery. Deleted rows are skipped; fetched rows reflect the subsequent
HASH read. This does not provide snapshot isolation or atomicity across rows.

## Wide confirmation: 100,000 rows × 105 columns

Five measured trials after warm-up, complete typed Arrow IPC output. Regular is
one reader; partition uses four readers and eight public ADBC descriptors.

| API | Merged #416 seconds (range) | Candidate seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 4.283 (4.227–4.310) | 2.771 (2.753–2.808) | 35.3% |
| partition | 2.759 (2.662–2.801) | 1.816 (1.781–1.856) | 34.2% |

Ranges are separated for both APIs. The separate matched-batching repeat below
confirms the improvement again. Timing includes spawned workers/imports,
connections, planning, complete fetch, safe normalization, IPC writes and teardown.
Every value and multiplicity is checked outside timing against independently
converted official ClickBench input.

| Metric, median | Regular baseline → candidate | Partition baseline → candidate |
|---|---:|---:|
| Client CPU seconds | 3.257 → 3.093 | 4.797 → 4.318 |
| Redis CPU seconds | 1.811 → 1.015 | 1.582 → 0.978 |
| First batch seconds | 2.926 → 1.862 | 1.360 → 0.944 |
| Max worker peak RSS MiB | 267.812 → 310.219 | 176.484 → 220.156 |
| Redis output MB (decimal) | 336.595 → 151.281 | 338.824 → 152.029 |

**Memory is higher**: approximately 268 → 310 MiB regular, 176 → 220 MiB per
peak worker for partitions. This follows the user's preference for elapsed speed.
RSS is each trial's maximum worker lifetime high-water mark, including imports,
not simultaneous aggregate worker memory or a byte bound. Client CPU sums parent
and reaped workers; Redis CPU/output counters include small control overhead.
CPU work overlaps elapsed time; subtracting CPU from wall time is not a wait-time
measurement.

## Native references and equal batch-size check

The first confirmation retains the historical native settings (1,000-row HMGET
pipelines, 1,869-row Search pages). Then both native settings are raised to 4,096
and five measured rounds repeat all ADBC/native methods together. All use hiredis,
RESP3, identical typed Arrow IPC and exact full-value validation.

| Path | Original native settings, one reader / four readers | Matched 4,096 settings, one reader / four readers |
|---|---:|---:|
| ADBC merged #416 | 4.283 / 2.759 s | 4.206 / 2.766 s |
| ADBC candidate | 2.771 / 1.816 s | 2.782 / 1.803 s |
| redis-py, known keys | 3.018 / 1.537 s | 3.044 / 1.487 s |
| redis-py, Search discovery + HMGET | 3.729 / 1.754 s | 3.266 / 1.517 s |

The driver is faster than these native references for one reader, but **native
four-reader scans remain faster**. This is not a universal client ranking.
Native profiles use a known schema, explicit dense row ranges, an immutable
non-null fixture, and omit generic SQL/catalog/readiness behavior. Known-key
reads omit Search discovery entirely. Native four-reader ranges are not ADBC
partitions. Native Arrow batches follow the pipeline rows; ADBC's Arrow default
remains 65,536 rows. First-batch metrics therefore represent different batch
sizes and must not be used as a direct equal-size comparison.

The matched repeat independently measures ADBC's improvement:

| API | Merged #416 seconds (range) | Candidate seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 4.206 (4.192–4.295) | 2.782 (2.767–2.804) | 33.9% |
| partition | 2.766 (2.687–2.834) | 1.803 (1.755–1.930) | 34.8% |

## Why fewer bytes now help

The 3-round screens isolate map and batching costs:

| Experimental variant | Regular baseline → candidate | Partition baseline → candidate |
|---|---:|---:|
| HMGET, merge maps, page 1,869 / pipeline 1,000 | 4.318 → 4.437 s | 2.781 → 2.216 s |
| HMGET, adopt maps, page 1,869 / pipeline 1,000 | 4.248 → 3.991 s | 2.794 → 2.109 s |
| HMGET, adopt maps, page/pipeline 4,096 | 4.317 → 2.797 s | 2.764 → 1.818 s |

The production candidate independently preserves narrower/generic pipeline sizes;
the isolated batched screen changed fetchRows' batch bound globally. Only the
production confirmation should be used to assess that final guard.

Command deltas from the matched repeat, median per complete trial:

| API | Cursor READ baseline → candidate | HMGET baseline → candidate | FT.INFO baseline → candidate |
|---|---:|---:|---:|
| regular | 53 → 24 | 0 → 100000 | 0 → 0 |
| partition | 48 → 24 | 0 → 100000 | 144 → 96 |

Search discovery and HASH fetch remain sequential stages per page. The larger
bounded pages/pipelines amortize extra round trips. Server output falls about
55%; much less repeated field-name serialization reduces Redis CPU. This is not
zero-copy: raw string maps, go-redis decoding and final typed Arrow work remain.

## Projection controls

The first 64 physical fixture columns select the production boundary:

| API | Merged #416 seconds (range) | Candidate seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 3.793 (3.783–3.935) | 2.051 (2.035–2.064) | 45.9% |
| partition | 2.539 (2.520–2.544) | 1.401 (1.360–1.418) | 44.8% |

Three-column control (`watchid, userid, searchphrase`), five-round repeat:

| API | Merged #416 seconds (range) | Candidate seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 2.172 (2.132–2.189) | 2.185 (2.092–2.228) | -0.6% |
| partition | 0.894 (0.887–0.988) | 0.894 (0.841–1.058) | 0.0% |

No narrow speed gain is claimed. The initial three-round narrow run had a higher
partition median (0.834 → 0.959 seconds); ranges were 0.763–0.945 / 0.950–0.968.
It was retained and investigated. The repeat is effectively flat at 0.894 seconds
for both, with overlapping 0.887–0.988 / 0.841–1.058 ranges. The same narrow
retrieval path remains selected.

## Controlled latency/bandwidth model and bidirectional bytes

The final 3-round model uses a userspace TCP proxy: **1 Gb/s per connection per
direction**, with 10 ms configured added RTT. This is independent full-duplex,
per-connection throttling, not a shared 1 Gb/s uplink across four workers.
Calibration observed approximately 12 ms pings and 8 MiB transfers near the
0.077-second ideal (logs retained). The proxy adds scheduling/CPU overhead;
its process CPU is not included in reported client or Redis CPU.

| API | Baseline median seconds (range) | Candidate median seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| regular | 7.217 (7.150–7.238) | 4.471 (4.420–4.511) | 38.0% |
| partition | 3.755 (3.661–3.965) | 2.653 (2.614–2.744) | 29.3% |

The earlier model run agrees, but shutdown revealed uncollected connection
tasks in the helper. Connection/task cleanup was fixed, calibration repeated,
and the complete timing matrix rerun. `link-final.json` is the canonical run;
`link-1000mbps-10ms.json` retains the earlier complete measurements.
Both matrices fully validated all values. The final helper shut down cleanly.

A separate one-measured-round diagnostic captures **both traffic directions**;
it is byte accounting, not an additional statistical speed claim:

| API/path | Request MB | Reply MB | Total MB |
|---|---:|---:|---:|
| regular/baseline | 0.006 | 336.598 | 336.604 |
| regular/candidate | 192.892 | 151.284 | 344.176 |
| partition/baseline | 0.021 | 338.827 | 338.849 |
| partition/candidate | 192.906 | 152.032 | 344.938 |

**A 55% reply reduction is not a 55% total-network reduction.** HMGET repeats field
names in requests: total traffic is about 2.3% higher regular and 1.8% higher
partition. Redistribution across full-duplex directions, cheaper replies/server
work and larger batches improve this model; asymmetric bandwidth can change the
tradeoff. No universal WAN speedup is established.

Reproduce with `python benchmarks/search-hmget/link.py --target 18111 --listen
18112 --mbps 1000 --rtt-ms 10`, then benchmark URI port 18112 without reloading.
The retained calibration program runs from the repository root and imports the
helper under `benchmarks/search-hmget`.

## Allocation and CPU diagnostics

Three ordinary Go-driver scans of the same fixture, using the existing read-only
profile helper. These use Go's Arrow allocator; primary native timings use the
C allocator. A single three-scan diagnostic is not additional statistical proof
of an elapsed win.

| Metric, three scans | Baseline | Candidate |
|---|---:|---:|
| elapsed | 11.956880375s | 7.64757775s |
| allocated_bytes | 5553863080 | 4925282496 |
| allocations | 128749046 | 99348398 |
| GCs | 140 | 103 |

Cumulative allocated bytes fall 11.3%, allocations 22.8%. This is compatible with
higher peak RSS: larger bounded batches keep more data alive at once. Sampled
CPU totals are 7.15 / 6.69 seconds; Arrow append/decode work is essentially
unchanged. Native end-to-end client/server CPU counters above establish the
primary cost comparison. Neither pprof nor command counts cleanly separates
server execution from socket wait in wall time.

The remaining allocation targets are concrete: candidate fetchRowsChunk raw maps
account for about 1,452 MiB sampled allocations, go-redis HMGet command construction
about 1,027 MiB, and reply slices about 516 MiB across three scans. Direct map
adoption removes the second copy, but these maps/command arguments still exist.
CPU and allocation profiles/top tables are retained for follow-up work; no
unmeasured benefit from a future optimization is claimed.

## Method, reproduction and limits

Redis 8.6.2 / Search 8.6.0 in Docker, four server CPUs / two GiB; localhost TCP
without TLS. macOS arm64, 18 host logical CPUs / 36 GiB. Go 1.27.2, same c-shared
release flags and v0.1.0 version linker value. Python/library versions and binary
hashes are in the raw JSON and manifest. Fixture, DDL source and upstream revision
match the [wide-transfer instructions](../../wide-transfer/README.md).
No task correctness suites/profiling ran during timed matrices. Methods reverse
order on alternating rounds. Every completed timed execution validates all
selected values and multiplicity after fully reading Arrow output. The canonical
validation table has 79,578,019 logical Arrow bytes; this is not RESP or IPC size.

Build both pinned sources using the release command in the manifest. With the
benchmark environment described in the parent instructions:

```sh
python benchmarks/wide-transfer/compare.py \
  --baseline /absolute/baseline.dylib --candidate /absolute/candidate.dylib \
  --uri redis://127.0.0.1:18111/0 --protocol 3 \
  --parquet /absolute/hits-prefix.parquet --clickbench /absolute/ClickBench \
  --work /tmp/confirm --load --repeats 5 --native-references \
  --native-pipeline 4096 --native-page 4096
```

Use a dedicated server: `--load` replaces transferbench.hits. Keep correctness
fixtures separate. For screening, `../prepare.py` makes a new exact-source copy;
`--adopt --page 4096 --pipeline 4096` reproduces the fast isolated variant. The
script asserts every transformation and never patches the production checkout.
Earlier variants accidentally excluded the regular implicit row-ID sort: those
regular samples were baseline-path controls, not HMGET evidence. Their complete
and interrupted artifacts are retained under `initial-scope/`. Revised screens
include row-ID order and clearly show reduced bytes on both APIs.

These prefix results do not establish 100-million-row transfer time, full-scale
ClickBench query performance, a universal projection threshold, a production WAN
ranking or speed on every Redis deployment. Cluster correctness is checked;
Cluster performance, Cloud/Software timing and other architectures are not measured.

## Verification and raw evidence

All **240 executions** in the 11 completed primary matrices passed full-value and
row-multiplicity validation (52 warm-ups, 188 measured runs). Initial-scope
attempts and Go profiles are separate. Raw data:

- Screens: [map merge](screen-merge-v2.json), [map adoption](screen-adopt-v2.json),
  [larger batches](screen-batched-v2.json).
- Confirmation: [wide plus original native references](confirm-wide.json),
  [64 columns](confirm-64.json), [initial narrow control](confirm-narrow.json).
- Fair batching repeat: [4,096-row native references](native-4096.json),
  [five-round narrow control](narrow-repeat.json).
- Network: [final calibrated model](link-final.json), [earlier model](link-1000mbps-10ms.json),
  [bidirectional byte diagnostic](bidirectional.json).
- Profiles: [summary](profile-summary.json), [baseline allocations](baseline-alloc.pprof),
  [candidate allocations](candidate-alloc.pprof), [baseline CPU](baseline-cpu.pprof),
  [candidate CPU](candidate-cpu.pprof), and corresponding top tables/logs.

The exact native candidate passes upstream ADBC validation on RESP3 standalone,
RESP2 standalone and three-primary Cluster: **371 passed, 8 existing capability
skips, 3 subtests passed per run**. These tests do not replace wide-query coverage.
Focused wide tests independently check every selected value across 5,000 sparse
IDs, aliases/duplicates, exact BIGINTs, NULs, timestamps, missing/default versus
explicit NULLs and an all-NULL row. They check regular implicit order, both public
APIs, deletion/update between discovery/fetch, empty results, LIMIT/OFFSET,
release/cancellation and retained Arrow records. Final standalone RESP2 and
Cluster RESP3 focused checks pass with the race detector.

The three proposed REVIEW.md passes found no remaining actionable correctness,
security/ownership or evidence blocker in the scoped change. No human approval
is inferred. CI, Cloud/Software and other-architecture results remain pending.

The final full Go run passes **1,008 test/subtest events**, with 15 existing skips
and zero failures across three packages. [Full JSON log](go-tests-final.jsonl)
and the [verification record](../../../docs/sdlc/changes/search-hmget-speed/validation.json)
retain package timings and skip names. Focused checks also passed under the race
detector. All three task-owned Docker servers and the proxy have been stopped;
this task did not modify unrelated TLS validation containers. No Cloud resources were created.
