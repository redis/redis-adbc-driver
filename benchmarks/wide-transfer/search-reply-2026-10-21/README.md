# Search reply speed: issue #409

Measured 2026-10-21. Current merged baseline `6495522e` includes #412's Arrow
memory optimization; candidate production source is `99c1141b`. The benchmark
runner's index-metadata fix is `521583a3`. [Manifest](manifest.json) pins source,
binary and profile identities. **RESP3 is already the driver default and remains
unchanged. Both ADBC and redis-py explicitly use RESP3 in these comparisons.**

## Change and measured target

Ordinary non-list Search rows now copy fields directly from go-redis's RESP3
attribute map or RESP2 alternating pairs into the owned raw string map. This
removes intermediate normalized maps. Known top-level RESP3 fields are looked
up directly rather than normalizing their wrappers. The final raw map and
go-redis's protocol decoder remain; this is not a zero-copy implementation.

The generic list-length reducer parser is retained to preserve its alias rules.
Accepted/malformed response shapes, duplicate-field last-wins behavior (including
unsupported final values), missing values, byte ownership and cursor IDs are
covered by compatibility tests. SQL planning, Redis commands, default batching,
Arrow construction and lifecycle/readiness guarantees are unchanged. Both public
ADBC APIs use the same cursor parser.

## End-to-end matched RESP3 results

100,000 rows × 105 columns; five measured rounds after warm-up, full typed Arrow
IPC output. Timing includes fresh spawned workers/imports, connections, planning,
complete reads, safe normalization, output writes and worker teardown.

| ADBC API | Baseline median seconds (range) | Candidate median seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| Regular, one reader | 4.585 (4.551–6.441) | 4.258 (4.240–5.217) | 7.1% |
| Partition, four readers/eight descriptors | 2.914 (2.874–3.787) | 2.799 (2.662–3.563) | 4.0% |

Wide client CPU falls 3.873 → 3.310 seconds (−14.5%) regular and 5.526 → 4.843
(−12.3%) partition. First batch medians improve 3.155 → 2.921 / 1.445 → 1.371
seconds. Peak worker RSS is effectively flat, approximately 267 / 175 MiB.
Redis output is unchanged at approximately 336.8 / 339.0 MB. Server CPU medians
are 1.778 → 1.775 / 1.596 → 1.628 seconds. The measured change targets client
decoding; it does not reduce server work or wire payload.

One round was slow and ranges overlap. A separate ADBC-only confirmation run is
recorded below; individual raw trials, including outliers, are retained.

### Confirmation: regular speedup repeats; partition elapsed gain is inconclusive

Another five measured rounds after warm-up, with identical data, binaries,
protocol and output, but no native reference trials between ADBC methods:

| API | Baseline median seconds (range) | Candidate median seconds (range) | Less elapsed time |
|---|---:|---:|---:|
| Regular | 4.570 (4.511–4.846) | 4.243 (4.216–4.341) | 7.2% |
| Partition | 2.805 (2.753–2.853) | 2.787 (2.732–2.794) | 0.6% |

The regular improvement repeats with separated timing ranges. **A reliable
partition elapsed-time improvement is not established**: the confirmation is
effectively flat despite lower client CPU (5.251 → 4.772 seconds, −9.1%).
Regular confirmation client CPU falls 3.815 → 3.262 seconds (−14.5%). Do not
present the first run's 4.0% partition median as a universal or confirmed gain.

For `watchid, userid, searchphrase` (three columns), regular medians are
2.150 → 2.183 seconds (+1.5%) and partition medians 0.891 → 0.916 (+2.8%).
Ranges overlap: regular 2.144–2.206 / 2.136–2.208, partition 0.808–0.981 /
0.891–1.120. No narrow elapsed-time improvement is claimed; the small higher
medians should not be hidden by the wide result.

## Current official native references

| Wide Arrow IPC path | One reader | Four readers/eight ranges |
|---|---:|---:|
| ADBC baseline | 4.585 s | 2.914 s |
| ADBC candidate | 4.258 s | 2.799 s |
| redis-py+hiredis, known keys | 3.056 s | 1.568 s |
| redis-py+hiredis, Search discovery + HMGET | 3.784 s | 1.867 s |

ADBC remains slower in this wide comparison. Native references use known schema,
bounded 1,000-command HMGET pipelines and an existence field on an immutable,
dense, non-null fixture. Discovery uses Search key/row-ID cursor pages. Native
four-reader profiles use explicit row ranges rather than ADBC descriptors and
omit generic SQL/catalog/readiness semantics. First-batch sizes differ (native
up to 1,000 rows; ADBC 65,536), so their first-batch metrics are not equal-size
comparisons. These are references, not interchangeable production APIs.

Earlier #396/#408 native comparisons explicitly selected RESP2 while ADBC used
its RESP3 default. Those historical numbers remain separate. Here `--protocol 3`
sets both clients and the harness records a live control connection's observed
RESP3 negotiation. Explicit RESP2 remains available with `--protocol 2`.

## Current-build profiling evidence

The read-only Go diagnostic performs three complete ordinary scans of the same
fixture. Baseline profile sources are the exact baseline archive plus the same
opt-in test helper. It uses the Go Arrow allocator, whereas native transfer
timings use the C Arrow allocator. It establishes where CPU/allocations occur,
not an exact native-library wall-time decomposition.

- Baseline `redisReplyMap` normalization alone accounts for 1,396 MiB / 20.6%
  of sampled allocated bytes. That site disappears from the candidate profile.
- MemStats allocated bytes fall 7,039,487,576 → 5,553,871,064 (−21.1%) across
  the three scans; allocations 129,949,047 → 128,749,046. These are cumulative
  allocations, not peak/live memory. GC count is 168 → 136 in this diagnostic.
- Sampled CPU on cursor-parser stacks falls 0.99 → 0.56 seconds. Overall CPU
  samples are 8.25 / 7.19 seconds. macOS runtime/OS activity is substantial;
  socket wait and server execution must not be labeled parser CPU.
- Diagnostic elapsed time is 12.899 → 12.041 seconds, a single three-scan run
  per build. The repeated native trials above are the elapsed-time evidence.

Raw [baseline CPU](baseline-cpu.pprof), [candidate CPU](candidate-cpu.pprof),
[baseline allocations](baseline-alloc.pprof), [candidate allocations](candidate-alloc.pprof),
top tables and profile logs are retained alongside the manifest.

The isolated 256-row, 105-string-column RESP3 parser microbenchmark changes
1.134 → 0.576 ms/op, 2,537,756 → 1,270,042 bytes/op, and 2,050 → 1,026
allocations/op. Both variants parse the same prebuilt complete replies; the
generic variant uses the unchanged list-aware normalizer with no matching list
alias. This excludes protocol decoding, server/network work and Arrow output.
The [raw microbenchmark](parser-microbench.txt) also records RESP2 and narrow rows.

## Reproduction and limits

Redis 8.6.2 / Search 8.6.0, dedicated Docker server with four CPUs/two GiB,
localhost TCP without TLS. Native macOS arm64 host: 18 logical CPUs / 36 GiB.
Go 1.27.2; Python, redis-py, hiredis and Arrow versions are in the raw results.
No task-owned correctness/profiling suite ran during the timed matrices.

All **120 executions in the completed matrices** passed independent exact-value
and row-multiplicity validation: 20 warm-ups and 100 measured trials.
[Wide results](wide-results.json), [narrow results](narrow-results.json),
[confirmation results](wide-confirm-results.json). The earlier harness failure
described below is separate and is not counted as a completed matrix.

The same independently converted official ClickBench 100K prefix and typed
value/multiplicity oracle are used for every path. Source SHA, upstream DDL
commit and release build flags are in the manifest. Loading/readiness and exact
validation are outside timing. Methods run in forward/reverse order on
alternating rounds. Client CPU sums parent/reaped-worker user/system time;
RSS is the median maximum worker lifetime high-water mark, not simultaneous
aggregate memory. Wire accounting includes small control-query overhead.

The native loader now handles RESP3 FT.INFO maps and the discovery reader handles
RESP3 result/attribute maps. A first warm-up attempt used an obsolete hardcoded
index after recreating the fixture. Six warm-ups had passed, zero measured
rounds completed; the failed attempt is retained under [initial-failure](initial-failure/results.json).
The corrected runner obtains the actual index from metadata. Completed matrices
come from separate directories; the driver did not fail correctness validation.
An earlier profile-loader FT.INFO shape error is also recorded in the manifest;
the data had loaded and all profile scans returned 100,000 rows.

Use the release command in the manifest for both sources. With the dependencies
and fixture described in the [parent instructions](../README.md):

```sh
python benchmarks/wide-transfer/compare.py \
  --baseline /absolute/path/baseline.dylib \
  --candidate /absolute/path/candidate.dylib \
  --uri redis://127.0.0.1:18089/0 --protocol 3 \
  --parquet /absolute/path/hits-prefix.parquet \
  --clickbench /absolute/path/ClickBench \
  --work /tmp/search-reply-wide --load --repeats 5 --native-references
```

`--load` replaces only `transferbench.hits`; use a dedicated server. Repeat in a
new directory with `--columns watchid userid searchphrase` and no `--load` for
narrow results. Omit `--native-references` for the ADBC-only confirmation. To
profile, use `REDIS_REPLY_PROFILE=1 REDIS_URI='redis://HOST?protocol=3' go test
-run '^TestSearchReplyScanProfile$' -count=1 -v -cpuprofile cpu.pprof -memprofile
alloc.pprof .` on the already loaded fixture, outside timing runs.

These local prefix results do not establish production/WAN performance,
full-scale ClickBench SQL speed or 100-million-row transfer time.
