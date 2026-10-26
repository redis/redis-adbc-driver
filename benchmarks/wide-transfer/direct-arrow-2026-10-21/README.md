# Direct Arrow construction: issue #408

Measured 2026-10-21. Baseline current-main source `cebe8088` (already includes
#404) versus candidate `85330bd6`. Exact binaries, production-file hashes,
compiler flags and environment are in [manifest.json](manifest.json).

Eligible managed-table simple projections now reuse one scratch row and append
decoded values to Arrow builders. They no longer retain a projected `[]Value`
for every result or perform a later row-buffer-to-Arrow traversal. Regular and
public partition readers use the same implementation. Generic plans, Redis
retrieval, default batch rows and SQL/readiness guarantees remain unchanged.

## Result: substantially less memory, no elapsed-time win

100,000 rows × 105 columns, median of five measured trials after warm-up:

| API | Baseline seconds (range) | Candidate seconds (range) | Baseline → candidate peak worker MiB |
|---|---:|---:|---:|
| Regular, one reader | 4.540 (4.494–4.672) | 4.560 (4.534–4.649) | 2,166 → 267 (−87.7%) |
| Public partition, four readers/eight ranges | 2.798 (2.692–2.935) | 2.831 (2.818–3.004) | 409 → 176 (−57.0%) |

Elapsed medians change +0.4% / +1.2%, with overlapping ranges. First-batch
medians are essentially flat: 3.074 → 3.114 seconds regular, 1.397 → 1.398
partition. **This is a memory/allocation improvement, not a transfer-speed
improvement.**

There is a CPU tradeoff: wide client CPU rises 3.412 → 3.809 seconds (+11.7%)
regular and 5.228 → 5.350 (+2.3%) partition. The cause has not been isolated;
do not infer a GC attribution from these counters alone. Server CPU is similar
(1.754 → 1.794 / 1.587 → 1.587 seconds), and incoming Redis output remains
approximately 336.6 / 338.9 MB. Raw Search reply parsing and maps are still
present; #409 is a separate next target.

For the three-column projection `watchid, userid, searchphrase`, regular time
is 2.153 → 2.149 seconds and partition time 0.963 → 0.938 seconds. Ranges
overlap; no material narrow elapsed-time change is claimed. Regular peak worker
RSS falls 174 → 125 MiB; partition RSS is essentially flat at 120 MiB.

RSS is the median of each trial's maximum worker lifetime high-water mark,
including imports. It is not simultaneous aggregate memory across workers or
Redis memory. The two-GiB server limit does not constrain host client memory.
Client CPU sums the parent and reaped worker user/system CPU within timing.

## Fresh official redis-py references

All paths return complete typed Arrow IPC from the same immutable fixture:

| Path | Wide, one reader | Wide, four readers | Narrow, one reader | Narrow, four readers |
|---|---:|---:|---:|---:|
| ADBC baseline | 4.540 s | 2.798 s | 2.153 s | 0.963 s |
| ADBC candidate | 4.560 s | 2.831 s | 2.149 s | 0.938 s |
| redis-py+hiredis, known keys | 3.099 s | 1.568 s | 1.653 s | 0.623 s |
| redis-py+hiredis, Search discovery + HMGET | 3.826 s | 1.762 s | 2.784 s | 0.880 s |

ADBC remains slower in this wide comparison even when the native reference
includes Search discovery. There is no universal ranking: the narrow
single-reader ADBC path is faster than the discovery reference, while known-key
native reads remain faster. The references omit generic SQL planning,
catalog/generation/readiness checks and missing/default/null semantics. They
require known schema and an immutable dense non-null fixture. They are not
production replacements for the driver.

Native reads use redis-py 8.1.0 with hiredis 3.5.0, bounded 1,000-command HMGET
pipelines and an extra `__rowid` existence field. The discovery variant obtains
keys/IDs with 1,869-row Search cursor pages and closes its cursors. Native
four-reader profiles use eight explicit 12,500-row ranges, not ADBC descriptors.
Native first batches have up to 1,000 rows; ADBC uses the default 65,536, so
first-batch values in raw files are not equal-batch-size comparisons.

## Allocation diagnostic

[microbench.txt](microbench.txt) compares complete decode/fit/coerce/Arrow
construction for 1,024 synthetic rows, with raw reply maps prebuilt and excluded:

| Columns | Buffered rows bytes/op → direct | Allocs/op buffered → direct |
|---|---:|---:|
| 105 string columns | 26,187,059 → 9,405,885 (−64.1%) | 4,712 → 2,956 |
| 3 string columns | 760,908 → 269,361 (−64.6%) | 1,140 → 98 |

This helper benchmark uses Go's Arrow allocator, whereas the native library
uses its C allocator. It does not measure whole-query allocations or prove an
end-to-end speedup. Its raw timing and allocation counts are retained.

## Method, validation and reproduction

- Redis 8.6.2 / Search 8.6.0, dedicated Docker server: four CPUs / two GiB.
  Native macOS arm64 host: 18 logical CPUs / 36 GiB. Localhost TCP, no TLS.
  No correctness suite ran concurrently with timed transfer trials.
- Same official 100,000-row ClickBench prefix and independent DDL/type conversion
  as the #396 fixture. Source SHA and upstream commit are in the manifest.
  Loading is excluded from timing.
- Every path/profile has one warm-up and five measured rounds, with forward and
  reverse method order on alternating rounds. Fresh spawned processes, imports,
  connections, planning where applicable, complete fetch, safe Arrow conversion,
  IPC file writes and worker teardown are included. Parent initial imports and
  full-value validation are excluded.
- Regular ADBC uses `execute` + `fetch_record_batch`. Public partition ADBC
  plans eight descriptors with `adbc_execute_partitions`, then four workers use
  `adbc_read_partition` + `fetch_record_batch` with a connection per task.
- **All 96 wide/narrow executions passed exact comparison of every selected
  value and row multiplicity**, including 16 warm-ups and 80 measured trials.
  [Wide raw trials](wide-results.json), [narrow raw trials](narrow-results.json).
  `logical_arrow_bytes` is the sorted validation-table byte count, not RESP or
  IPC file bytes. Wide canonical validation is 79,578,019 bytes; the unsorted
  source is 79,228,019 bytes.
- These local prefix results do not establish WAN performance, production
  behavior, full ClickBench SQL performance or 100-million-row transfer time.

Use the release build command in the manifest for both source revisions, and
the dependencies in the raw results' `environment` object. The prefix and
upstream DDL setup are described in the [parent instructions](../README.md).

```sh
python benchmarks/wide-transfer/compare.py \
  --baseline /absolute/path/baseline.dylib \
  --candidate /absolute/path/candidate.dylib \
  --uri redis://127.0.0.1:18089/0 \
  --parquet /absolute/path/hits-prefix.parquet \
  --clickbench /absolute/path/ClickBench \
  --work /tmp/direct-arrow-wide --load --repeats 5 --native-references
```

Use a dedicated server: `--load` replaces `transferbench.hits`. Repeat with a new
work directory and `--columns watchid userid searchphrase`, omitting `--load`,
for narrow results. Retain raw trials, failures and actual environment identities.
