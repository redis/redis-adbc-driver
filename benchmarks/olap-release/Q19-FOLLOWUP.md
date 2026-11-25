# Focused Q19 follow-up

Measured 2026-11-23. Only TPC-H SF1 Q19 and a six-row integration smoke were run; full SSB/TPC-H/TPC-DS suites remain stopped. This is not a full-suite score.

| Execution path | Q19 result | Median |
|---|---|---:|
| Native DuckDB 2.0.1 | Warm-up + 3 timed runs validated | 8.437 ms |
| Direct Redis ADBC, v0.0.31 + Q19 optimizer patch | Warm-up + 3 timed runs validated | 12.556 s |
| DuckDB over Redis ADBC (`adbc_scanner`) | Warm-up interrupted at 60-second deadline; no timed runs | — |

Direct Redis samples: warm-up 15.971 s; timed 16.552, 12.556, 12.327 s. Reference revenue: `3083843.0578`. Results were fully fetched and validated as row multisets with relative numeric tolerance 1e-7 and absolute tolerance 1e-6. No per-engine client-cgroup OOM increments were observed. Engine processes were verified distinct; reference export and bounded disk-backed validation ran outside timing in separate processes.

The driver was built from release commit `d68f9a21ee7a4b5a3572926ed29870c61ceb984e` with only the Q19 common-condition patch, excluding unrelated working-tree changes. GetInfo reported `v0.0.31-q19fix`; native library SHA-256 `f3637eb3a41d5286a954ed33b11916751194d210250fd8fb194a9f423e8b8801`. Redis Software used Redis 8.8.0, one RAM shard, noeviction, authenticated TLS, and active/effective 4× QPF (six Search workers, mnp routing, 32 connections), reverified before execution. The Linux ARM64 client shared the server network namespace with four CPU quota and an 8 GiB memory cap; server container cap was 20 GiB and database capacity 14 GiB. DuckDB used four threads and an 8 GB internal limit. Server CPU work was additional; these are not equal-total-CPU comparisons.

Only `part` (200,000 rows) and `lineitem` (6,001,215 rows) were loaded from the retained identical SF1 source and independently count-validated. Historical Q19 used an 18 GiB database allocation and a shared engine worker; this follow-up uses 14 GiB and separate processes. Thus the result establishes focused deadline completion and correctness, not a speedup caused solely by the optimizer. Historical samples remain unchanged and qualified.

## Third execution path

The live six-row smoke passed with DuckDB 2.0.1 and signed Query.Farm adbc_scanner revision `98ff0a39fc`. It matched direct Redis ADBC on filtered grouping, exact decimals, duplicates, NULL group keys and NULL aggregate inputs. EXPLAIN showed an ADBC table scan with filter delivery and a DuckDB hash aggregate. An initial assertion used an older plan-label spelling; normalizing that assertion corrected the test, and the result rows already matched.

The third path attaches a read-only Redis catalog using the same native library/options as direct Redis ADBC. It executes the unchanged SQL with whole-query remote pushdown disabled (`query_pushdown false`), leaving joins/aggregation to DuckDB. Its SF1 Q19 warm-up was interrupted at the configured 60-second deadline (`INTERRUPT Error: Interrupted!`), with no validated samples or ratio. The plan exposes source restrictions plus local filters; the plan alone does not prove which predicates actually executed remotely. Root-cause ownership remains unestablished, so no upstream defect is asserted.

## Evidence and tracking

- [Raw samples](results/q19-fixed/tpch/queries.jsonl), [provenance/settings](results/q19-fixed/tpch/metadata.json), [unchanged SQL](results/q19-fixed/tpch/queries/19.sql).
- [Third-path smoke](results/q19-fixed/third-live-smoke.json), [Q19 third-path plan](results/q19-fixed/tpch/third-explain.txt), [generated result table](results/q19-fixed/RESULTS.md).
- [Original direct Q19 issue #586](https://github.com/redis/redis-adbc-driver/issues/586); [optimizer PR #597](https://github.com/redis/redis-adbc-driver/pull/597).
- [Isolation issue #596](https://github.com/redis/redis-adbc-driver/issues/596); [harness PR #598](https://github.com/redis/redis-adbc-driver/pull/598).
- [Third-path Q19 performance issue #599](https://github.com/redis/redis-adbc-driver/issues/599).
  Note added 2026-11-24: #599 closed that day on 10% subset results after
  [PR #637](https://github.com/redis/redis-adbc-driver/pull/637). No SF1
  third-path rerun is recorded here; [#648](https://github.com/redis/redis-adbc-driver/issues/648)
  tracks it. The measurements below are unchanged.

No other benchmark queries were run. Operational identifiers, credentials, runtime logs and generated datasets remain private.

## Fresh build of merged main

At the user's request, commit `b9253c82076257d9c6db24e2fc8d62ff3dc450f1` was compiled again as `v0.0.31-main.b9253c82` and Q19 alone was repeated across all three paths. GetInfo confirmed that version; library SHA-256 `e1acddf98fe36c07bb6cb070de16d09a8d849a4529e4945b44f1727599664640`.

| Execution path | Result | Median |
|---|---|---:|
| Native DuckDB 2.0.1 | Warm-up + 3 validated timed runs | 8.577 ms |
| Direct Redis ADBC, fresh merged-main build | Warm-up + 3 validated timed runs | 3.762 s |
| DuckDB over Redis ADBC | Warm-up interrupted at 60-second deadline; no timed runs | — |

Direct Redis warm-up: 7.266 s; timed samples: 3.646, 3.762, 4.686 s. No engine client-cgroup OOM increments. Scanner revision remains `98ff0a39fc`. The fresh build does **not** resolve the third-path timeout ([#599](https://github.com/redis/redis-adbc-driver/issues/599)).

The same retained source supplied the two reloaded/count-validated tables. AOF persistence every second is now enabled; capacity/topology/client limits and effective 4× QPF were verified before execution. Reload/cache and persistence differences mean the changed direct latency is not a controlled speedup measurement. No other benchmark queries or full suites were run.

[Fresh-build samples](results/q19-main-b925/tpch/queries.jsonl), [provenance](results/q19-main-b925/tpch/metadata.json), [result table](results/q19-main-b925/RESULTS.md).
