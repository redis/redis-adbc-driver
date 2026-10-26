# Native IPC versus PyArrow: matched performance

Measured October 22, 2026, using the merged native IPC (#458) and incremental
UNION ALL (#462) implementations. Redis Software Enterprise 8.6.2 with verified
active QPF 4x, six Search workers, mnp routing and 32 connections. Same pinned
native binary for both methods; no driver production changes in this work.

## Findings

**These runs do not demonstrate a native IPC speed advantage.** Across 12 matched
workload/format comparisons, native median elapsed changes range from **-3.3% to
+3.8%** relative to PyArrow. All five-trial elapsed ranges overlap. This is a
small local experiment, not a formal equivalence test or a serializer-only test.

Native wide exports have slightly lower median process RSS in this matrix:
about 3–12 MiB less. Wide imports use about 2–3 MiB more. This does not establish
universal memory savings. CPU medians are close and vary by case; there is no
uniform CPU improvement.

The native interface is validated for keeping IPC handling inside the driver.
Choose it for that API/deployment convenience; these results do not justify
replacing historical performance numbers with a claim that it is faster.

**144/144 operations validated**, including 24 warmups and 120 measured fresh
processes. Every value and schema was checked across **7,202,328 output/read-back
rows**. Source input files are identical between import methods. Paired exports
have identical decoded schema hashes and batch counts, with small encoded-size
differences recorded below. No failed or skipped operation entered the summary.

## Complete-operation elapsed time

Five measured processes per method/case, after one warmup per method/case.
Each cell is median (minimum–maximum). Negative native change means a lower
median elapsed time, not a statistically established advantage. The comparison
uses the ratio of the two medians; raw paired ratios are in summary.json.

| Direction / workload | Format | Native seconds: median (range) | PyArrow seconds: median (range) | Native elapsed change |
|---|---|---:|---:|---:|
| export / aggregate-narrow | file | 0.2080 (0.2010–0.2110) | 0.2118 (0.2031–0.2159) | -1.8% |
| export / aggregate-narrow | stream | 0.2119 (0.2113–0.2175) | 0.2069 (0.1992–0.2174) | +2.4% |
| export / scan-narrow | file | 0.2517 (0.2498–0.2689) | 0.2603 (0.2530–0.2650) | -3.3% |
| export / scan-narrow | stream | 0.2520 (0.2446–0.2585) | 0.2579 (0.2525–0.2684) | -2.3% |
| export / scan-wide | file | 1.1894 (1.1552–1.2342) | 1.2051 (1.1712–1.2231) | -1.3% |
| export / scan-wide | stream | 1.2045 (1.1965–1.2535) | 1.1935 (1.1792–1.2215) | +0.9% |
| export / union-wide | file | 2.4750 (2.4446–2.4792) | 2.4746 (2.4378–2.5534) | +0.0% |
| export / union-wide | stream | 2.4736 (2.4224–2.4998) | 2.4772 (2.4385–2.4882) | -0.1% |
| import / import-narrow | file | 0.2036 (0.1928–0.2115) | 0.1977 (0.1936–0.2239) | +2.9% |
| import / import-narrow | stream | 0.1939 (0.1917–0.1994) | 0.1966 (0.1922–0.1975) | -1.4% |
| import / import-wide | file | 1.1694 (1.1545–1.4239) | 1.1860 (1.1608–1.3484) | -1.4% |
| import / import-wide | stream | 1.2365 (1.1814–1.2458) | 1.1915 (1.1665–1.3762) | +3.8% |

Negative elapsed change means a lower native median; 12/12 elapsed ranges overlap.

| Workload / format | Method | Output rows/s | IPC MiB/s | Client CPU seconds | Peak RSS MiB | Encoded MiB |
|---|---|---:|---:|---:|---:|---:|
| aggregate-narrow / file | native | 466 | 0.02 | 0.1346 | 104.5 | 0.0033 |
| aggregate-narrow / file | pyarrow | 458 | 0.02 | 0.1382 | 105.5 | 0.0033 |
| aggregate-narrow / stream | native | 458 | 0.01 | 0.1362 | 102.1 | 0.0031 |
| aggregate-narrow / stream | pyarrow | 469 | 0.01 | 0.1373 | 104.5 | 0.0031 |
| scan-narrow / file | native | 198,646 | 10.28 | 0.1326 | 104.5 | 2.5888 |
| scan-narrow / file | pyarrow | 192,095 | 9.95 | 0.1347 | 104.2 | 2.5888 |
| scan-narrow / stream | native | 198,417 | 10.27 | 0.1309 | 102.5 | 2.5882 |
| scan-narrow / stream | pyarrow | 193,875 | 10.04 | 0.1371 | 105.0 | 2.5882 |
| scan-wide / file | native | 42,039 | 34.08 | 0.7285 | 174.7 | 40.5357 |
| scan-wide / file | pyarrow | 41,489 | 33.64 | 0.7416 | 182.9 | 40.5352 |
| scan-wide / stream | native | 41,510 | 33.65 | 0.7253 | 178.8 | 40.5339 |
| scan-wide / stream | pyarrow | 41,895 | 33.96 | 0.7379 | 181.5 | 40.5334 |
| union-wide / file | native | 40,404 | 32.75 | 1.6598 | 225.9 | 81.0662 |
| union-wide / file | pyarrow | 40,410 | 32.76 | 1.6548 | 238.3 | 81.0651 |
| union-wide / stream | native | 40,427 | 32.77 | 1.6517 | 214.7 | 81.0641 |
| union-wide / stream | pyarrow | 40,368 | 32.72 | 1.6689 | 222.9 | 81.0630 |
| import-narrow / file | native | 245,604 | 12.72 | 0.0771 | 82.6 | 2.5891 |
| import-narrow / file | pyarrow | 252,848 | 13.09 | 0.0765 | 84.6 | 2.5891 |
| import-narrow / stream | native | 257,906 | 13.35 | 0.0748 | 82.3 | 2.5886 |
| import-narrow / stream | pyarrow | 254,375 | 13.17 | 0.0767 | 83.7 | 2.5886 |
| import-wide / file | native | 42,757 | 34.67 | 0.4529 | 155.7 | 40.5449 |
| import-wide / file | pyarrow | 42,157 | 34.19 | 0.4486 | 153.1 | 40.5449 |
| import-wide / stream | native | 40,438 | 32.79 | 0.4618 | 155.7 | 40.5431 |
| import-wide / stream | pyarrow | 41,962 | 34.03 | 0.4475 | 153.5 | 40.5431 |

CPU, RSS and throughput above are medians of the five operations. Output rows/s
means delivered rows: the aggregate emits only 97 groups after reading 50,000
input rows, so its output throughput is not comparable to scan input throughput.
IPC MiB/s uses actual encoded file bytes, not Redis wire bytes or Arrow payload
bytes. RSS includes the Python process, native Go driver and Arrow libraries.
See summary.json for ranges for every metric and separate user/system CPU.

## Matched paths and workloads

| Direction | Native | PyArrow client |
|---|---|---|
| Export | `adbc.redis.ipc.output`, format, `execute_update()` | `execute_query()` → Arrow C Stream → PyArrow IPC writer |
| Import | `adbc.redis.ipc.input`, format, `execute_update()` | PyArrow OSFile IPC reader → BindStream → `execute_update()` |

Both paths use the same source data, SQL, driver binary, 4,096-row output/input
batch policy, uncompressed IPC, filesystem and Redis deployment. The byte budget
is disabled and client query workers are fixed at one. Imported targets are
fresh tables with the same default all-indexable-column policy; table and index
creation are included in import time. Each method consumes the exact same
source IPC file for each format. File import adapters yield one batch at a time.

- **Narrow scan:** 50,000 rows, three columns: BIGINT id, INTEGER bucket, and
  variable text containing unicode, NULs and NULLs.
- **Wide scan:** 50,000 rows, 32 columns: the narrow fields plus exact decimal,
  UTC microsecond timestamp, double, nullable boolean, variable binary and 24
  nullable text fields. No floating-point approximation is used to validate
  integers or decimals.
- **UNION ALL:** two complete wide-table branches, 100,000 delivered rows,
  retaining duplicates and source order for independent validation.
- **Blocking aggregate:** 50,000 narrow rows, driver-side reduction with
  `aggregate_pushdown=none`, 97 groups ordered by bucket. Query explicitly casts
  SUM(id) to DECIMAL(38,0). Independent integer arithmetic supplies expected
  counts and sums; there is no index-aggregate shortcut.
- **Imports:** 50,000 narrow/wide rows from independent generated Arrow sources,
  in 13 IPC batches per file. Each imported table is queried back completely.

Scans emit 13 batches, UNION ALL 25, and the aggregate one; export batch counts
match between methods. Import read-back validation uses the normal query batch
size and is separate from the 13 encoded input batches.

## Measurement boundary and limitations

- Every operation runs in a fresh process. Source generation/loading, Python module imports,
  connection creation and statement/options setup precede timing. Export timing
  includes execution, all batch writing and complete output file close. Import
  timing includes IPC open/decode/binding, table/index creation, all batch writes
  and input close. It ends when the interface reports completion, not at a
  separately measured index-readiness or durable-storage boundary.
- Process user/system CPU deltas and `ru_maxrss` are captured immediately after
  the operation, **before validation or statement/connection teardown**. The
  recorded pre-operation RSS high-water baseline is not subtracted. Validation
  can allocate a complete reference/output table later; those allocations do
  not enter the recorded operation peak. The parent fixture loader and Redis
  server CPU/RSS are excluded.
- The PyArrow export path includes C Stream release, whose driver callback
  invokes Go `runtime.GC()`. Native export uses the native reader release path.
  These are actual interface-path costs. The comparison does not isolate IPC
  encoding cost, GC cost, the C boundary or Redis query time individually.
  Fresh processes may defer some runtime cleanup beyond measured completion;
  this is not a steady-state service CPU measurement.
- Both child processes import PyArrow for subsequent validation, including the
  native method. Native RSS therefore does not represent a minimal application
  that omits PyArrow entirely.
- All output files are fresh temporary files on the same local APFS volume.
  Local filesystem page cache is enabled; no fsync, cold-cache purge, crash
  durability, pipe, socket, WAN or TLS transport is tested. Native and PyArrow
  encoded bytes can differ slightly while their schemas and values match.
- One warmup and five measured fresh processes per method/case; method order
  alternates within each pair and starting order alternates by workload. The
  Redis server and input files remain warm/reused. This is a shared macOS arm64
  host (18 logical CPUs, 36 GiB), without dedicated host isolation.
- Dedicated local Docker database: Redis Software Enterprise 8.6.2, one primary
  shard, 1 GiB, noeviction, persistence disabled. Product/QPF/effective worker
  settings were checked before and after the matrix. RESP3 localhost TCP.
- Client: Python 3.12.14, PyArrow 26.0.0, ADBC manager 1.12.0. Pinned native
  library: Go compiler **1.27.2**, Arrow Go 18.8.0. The actual compiler comes from
  binary build information, not the lower minimum declared in go.mod.
- These are synthetic fixtures, not ClickBench or a rerun of all historical
  transfer tests. There are no concurrent clients, cross-platform runtime,
  compressed IPC, dictionary data or scale-out performance claims.

The pinned binary embeds revision `d4cbccf` with `vcs.modified=true`: it was
built with the UNION ALL changes subsequently merged in #462. Its production
Go source matches merged `e2e7725`; this was not a clean rebuild from that commit.
The manifest pins the binary and source hashes, and build-info.txt retains the
actual embedded metadata.

## Reproduce and evidence

Build with `make -C go build`, install the local Python package plus PyArrow,
and provide an owned disposable Redis Software database with verified effective
QPF 4x. From the repository root:

```sh
PYTHONPATH=python REDIS_ADBC_LIBRARY="$PWD/go/build/libadbc_driver_redis.dylib" \
python benchmarks/native-ipc/run.py \
  --uri redis://127.0.0.1:36379/0 --rows 50000 --repeats 5 \
  --output /absolute/new-output-folder
python benchmarks/native-ipc/summarize.py /absolute/new-output-folder
```

Adjust the library suffix for your platform; the measurements here are macOS
only. The runner creates unique `it_ipc_bench_*` and `it_ipc_import_*` tables,
checks all outputs, drops tables, and removes its temporary files. This task's
owned Compose fixture `adbc-ipc-bench` is removed after checks; no paid deployment
was created. Do not point the runner at a production database.

- [Raw operations](results.json): all 144 operations, including warmups.
- [Separate smoke results](smoke-results.json): 48 validated small-fixture operations, excluded from the performance summary.
- [Summary statistics](summary.json) and [generated tables](tables.md): medians,
  ranges, paired elapsed ratios and schema/batch parity checks.
- [Source input hashes and sizes](inputs.json).
- [Manifest](manifest.json), [native build information](build-info.txt), and
  [verification/review record](../../../docs/sdlc/changes/native-ipc-performance/evidence.md).
