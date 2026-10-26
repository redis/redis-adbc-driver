# Wide-result transfer

The [direct HASH-to-Arrow follow-up](../hash-slice-arrow/2026-10-21/README.md)
compares merged #419 with positional HMGET decoding. It retains the same Redis
commands and batching, and reports elapsed time, CPU, memory, allocation profiles,
tuned native references and repeated narrow controls.

The [wide HASH-fetch follow-up for #411](../search-hmget/2026-10-21/README.md)
compares merged #416 with bounded Search discovery plus HMGET and direct map
adoption. It includes regular/partition results, native references at both
historical and matched batch sizes, narrow controls and a controlled link model.

The [Search reply follow-up for #409](search-reply-2026-10-21/README.md) measures
client parser speed against merged #412, explicitly using RESP3 for both ADBC
and official redis-py references. It includes CPU/allocation profiles, raw
wide/narrow trials and a separate confirmation run. RESP3 was already the
driver default; this change does not switch protocols.

The [direct Arrow follow-up for #408](direct-arrow-2026-10-21/README.md)
replaces buffered projected rows with Arrow construction using one reusable
scratch row. Its separate current-main comparison and official redis-py
references report memory, allocation and elapsed-time tradeoffs. The #396
measurements below are retained as historical evidence, not the latest driver
comparison.

Simple column projections on managed tables now decode HASH values directly into prebound output slots. Regular streamed queries and partition readers share the optimized reader. This removes the typed per-row name map and generic select-list evaluation while preserving stored-value validation, missing/default/null handling, aliases, duplicate columns, exact integers, row existence checks, cursor readiness/generation checks, cancellation, limits and offsets. Residual predicates, expressions, late materialization, tie sorting, adopted/foreign/application tables and other unsupported shapes retain generic execution.

At the #396 revision, the pending Arrow batch size and Redis retrieval strategy
were unchanged, and raw Search reply maps and projected `Value` rows remained.
The #408 follow-up removes the projected row collection; raw reply allocation
and a byte-aware batch policy remain separate follow-ups (#409 and #410).

## Paired local results, 2026-10-21

Main baseline `20de26967b9974fa344f15de2b71491039321d86` versus implementation `7d373ac578fde7801a996490c8db1c1953f8f6d9`, compiled with identical release flags and Go 1.27.2 on native macOS arm64. The manifest pins the native binaries and changed production source files by SHA-256.

All runs read the same immutable official ClickBench prefix: **100,000 rows in a 105-column managed HASH table**. The wide result returns all columns; the narrow result selects `watchid, userid, searchphrase`. Each profile/build has one warm-up and five measured trials, with reversed method order on alternating rounds. All **48 executions** passed independent comparison of every returned value and row multiplicity against the typed source. Loading and validation are outside timing.

| Projection / API | Baseline median seconds (range) | Optimized median seconds (range) | Change in median time |
|---|---:|---:|---:|
| 105 columns / regular | 5.270 (5.213–5.436) | 4.525 (4.513–4.608) | −14.1% |
| 105 columns / partition | 2.945 (2.922–3.141) | 2.705 (2.634–2.741) | −8.2% |
| 3 columns / regular | 2.246 (2.182–2.277) | 2.203 (2.188–2.219) | −1.9% |
| 3 columns / partition | 0.926 (0.900–1.024) | 0.929 (0.788–0.989) | +0.3%; effectively flat |

Narrow timing ranges overlap substantially. Five repeats support the local wide-query improvement, not a production ranking or a precise universal speedup.

| Wide-result diagnostic, median | Regular baseline → optimized | Partition baseline → optimized |
|---|---:|---:|
| Client CPU seconds | 4.304 → 3.357 (−22.0%) | 6.343 → 5.096 (−19.7%) |
| First batch seconds | 3.572 → 3.068 | 1.489 → 1.350 |
| Max worker peak RSS MiB | 2,167 → 1,920 | 467 → 406 |
| Redis process CPU seconds | 1.834 → 1.771 | 1.579 → 1.571 |
| Redis output bytes, approximately | 336.8 MB → 336.8 MB | 339.0 MB → 339.0 MB |

RSS is the median of each run's maximum worker lifetime high-water mark, including imports. It is not simultaneous aggregate RSS across workers, a memory bound, or server memory. Client CPU sums parent and reaped worker user/system CPU during the timed interval. Redis INFO counters include small control-query overhead; the server has no other clients during measurement. Server transport is effectively unchanged, consistent with the client-side scope of this change.

A synthetic 105-string-column decoding/projection microbenchmark changes **37,000 → 16,384 allocated bytes/row** and **112 → 1 allocations/row**. This is a separate helper benchmark, not an end-to-end allocation count or a ClickBench timing claim; see `2026-10-21/microbench.txt`.

## APIs and timing

- Regular: one fresh worker uses `cursor.execute()` followed by `fetch_record_batch()`.
- Partition: the parent uses `adbc_execute_partitions()` with eight descriptors; four fresh workers consume them using `adbc_read_partition()` and `fetch_record_batch()`. Each partition contains approximately 12,500 rows. Connections are opened for each task.
- Timing includes partition planning, worker startup/imports, connections, full fetch, safe Arrow normalization, identical Arrow IPC file output, close and teardown. The parent benchmark process's initial imports are outside timing. Main-process library initialization is warmed before measured rounds.
- Source conversion uses the tracked ClickBench PostgreSQL DDL/type helper, including epoch-day dates and epoch-second timestamps. Validation sorts all selected fields, compares complete Arrow tables and preserves duplicate multiplicity.
- The source has 79,228,019 Arrow bytes before canonical sorting. The wide raw results' `logical_arrow_bytes` is the canonical validation table's 79,578,019 bytes, including its validity-buffer layout. These are not RESP bytes or serialized IPC file sizes.
- Redis 8.6.2 / Search 8.6.0, Docker limited to 4 CPU cores and 2 GiB RAM; localhost TCP without TLS. Host clients share 18 logical CPUs and 36 GiB RAM without a per-client quota. Correctness suites ran separately from these timing runs.

The sequential 100K prefix is a small local fixture. The results do not establish WAN performance, 100-million-row transfer time, or performance for expressions/residual filters that use the generic path.

## Reproduce

Use a dedicated server: `--load` replaces **only the benchmark table `transferbench.hits`**. Keep the Go/Python correctness suites on a separate clean server; discovery tests inspect bounded samples of the whole keyspace.

The [ClickBench instructions](../clickbench/README.md) describe fetching the official DDL and Parquet data. The runner reads only the first 100,000 rows, even if supplied the full file. The measured prefix SHA-256 is `d053397f7aa5b2e1ee9160779a9e9307b162c6994aebba829c7ea1411076c4d0`; upstream DDL commit is `ccda3033f3c7303d9d7f9634d6dae118ef085f96`.

Build the base commit in a separate checkout and this branch using the same command from each `go/` directory:

```sh
CGO_ENABLED=1 go build -buildmode=c-shared -tags driverlib \
  -ldflags '-s -w -X github.com/adbc-drivers/driverbase-go/driverbase.infoDriverVersion=v0.1.0' \
  -o /absolute/path/to/output.dylib ./pkg
```

Use the appropriate shared-library suffix for your platform. Python dependencies for the measured run were CPython 3.13.16, `adbc-driver-manager==1.12.0`, `pyarrow==26.0.0`, and `redis==8.1.0`.

```sh
python benchmarks/wide-transfer/compare.py \
  --baseline /absolute/path/to/baseline.dylib \
  --candidate /absolute/path/to/candidate.dylib \
  --uri redis://127.0.0.1:17979/0 \
  --parquet /absolute/path/to/hits-prefix.parquet \
  --clickbench /absolute/path/to/ClickBench \
  --work /tmp/redis-wide-comparison --load --repeats 5
```

Repeat with a new `--work` directory and `--columns watchid userid searchphrase`, omitting `--load`, for the narrow projection. The same runner validates every execution and retains raw trials and failures. `2026-10-21/` contains measured raw results, source/binary identities and allocation evidence. Re-run with your own environment and preserve its identities before making deployment decisions.
