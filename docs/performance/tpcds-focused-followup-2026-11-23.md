# Focused TPC-DS SF1 follow-up

Evidence snapshot: 2026-11-24 (updated through the separate Source 13 three-path Q4 attempt). This focused development-driver follow-up is separate from the [historical released-driver baseline](../../PERFORMANCE.md). Full suites remain stopped.

All eight targeted queries (Q2–Q9) now have fully fetched, independently validated results under the recorded artifacts below. Q4 completed under Candidate 12 after earlier failed attempts. This does not mean all eight passed on one build or that all 99 TPC-DS queries ran. These selected per-query observations span different builds; they are not a completed full-suite score, a single-build pass rate or isolated attribution to individual optimizer PRs. Full benchmark suites remain stopped.

## Selected validated focused observations

Native DuckDB medians come from the same per-query attempt as the Redis observation. Ratios are Redis median divided by that native control median; greater than 1 means Redis took longer. Failed Redis cases have no Redis median or ratio. Failed-case result dimensions belong only to the reference and are not shown as Redis output.

| Query / issue | Outcome | Artifact | Validated Redis shape | Redis warm-up (s) | Redis median (s) | Native DuckDB median (ms) | Redis / DuckDB |
|---|---|---|---:|---:|---:|---:|---:|
| [Q2 / #588](https://github.com/redis/redis-adbc-driver/issues/588) | Validated | Candidate 6 | 2513 × 8 | 54.661473 | 25.844439 | 13.198 | 1958.16× |
| [Q3 / #589](https://github.com/redis/redis-adbc-driver/issues/589) | Validated | Candidate 2 | 89 × 4 | 4.614877 | 3.162769 | 6.819 | 463.81× |
| [Q4 / #590](https://github.com/redis/redis-adbc-driver/issues/590) | Validated | Candidate 12, separate 16-CPU profile | 8 × 4 | 38.604606 | 43.873090 | 41.624 | 1054.04× |
| [Q5 / #591](https://github.com/redis/redis-adbc-driver/issues/591) | Validated | Candidate 6 | 100 × 5 | 8.051566 | 3.183503 | 15.494 | 205.46× |
| [Q6 / #592](https://github.com/redis/redis-adbc-driver/issues/592) | Validated † | Baseline | 45 × 2 | 60.908683 † | 36.146640 | 10.832 | 3336.99× |
| [Q7 / #593](https://github.com/redis/redis-adbc-driver/issues/593) | Validated | Candidate 7 | 100 × 5 | 27.582920 | 22.732101 | 22.592 | 1006.21× |
| [Q8 / #594](https://github.com/redis/redis-adbc-driver/issues/594) | Validated | Candidate 8 | 5 × 2 | 7.962310 | 5.725763 | 10.249 | 558.69× |
| [Q9 / #595](https://github.com/redis/redis-adbc-driver/issues/595) | Validated | Candidate 6 | 1 × 5 | 57.795611 | 30.745337 | 38.002 | 809.04× |

† **Q6 warm-up caveat:** its measured warm-up was **60.908682819 seconds end to end**, including execute, fetch and cursor cleanup, despite the configured 60-second statement deadline. It is not evidence of a warm-up finishing within 60 seconds. Its three timed outputs completed and validated separately. Q2 and Q9 also had warm-ups near the configured limit; the small sample does not establish a dependable deadline margin.

## Fully fetched Redis timed samples

| Query | Timed samples (s) |
|---|---|
| Q2 | 27.849848513 / 25.844439345 / 22.281323718 |
| Q3 | 3.211921584 / 3.162769252 / 3.103696251 |
| Q4 | 43.049607144 / 43.873090352 / 45.976894605 |
| Q5 | 3.463930335 / 2.881045418 / 3.183502876 |
| Q6 | 36.146640475 / 55.873529150 / 27.527829138 |
| Q7 | 22.649948093 / 22.770575885 / 22.732100928 |
| Q8 | 5.698030211 / 5.725762628 / 5.736935920 |
| Q9 | 30.745336598 / 43.636604686 / 29.511384347 |

## Recorded native identities

These are development/main artifacts, not relabeled release-driver measurements. Candidates 1–12 are combined investigation builds. Preserve these identities when carrying values into the final report.

- **Baseline — `v0.0.31-main.07b24edb`**: source `07b24edb3c782037f48670f6cc499d66256a59e0`; native-library SHA-256 `b87d295b6a84079a01bafc9f2c6f66f8db0a238fdb30cd449eb3e8eba0db98e0`.
- **Candidate 1 — `v0.0.31-dsfix1`**: source `c9c56f0678541ceeb321c81df27c46eb531f289a`; native-library SHA-256 `41d783c7812c4b4e8d720eeea293f06e5179f244aa745e08e8083dcc7c52f66d`.
- **Candidate 2 — `v0.0.31-dsfix2`**: source `2017ba09721c3cc4c284b0d0f3ac98693df2d2d7`; native-library SHA-256 `da4177d6077a20b87bf591a31c0873afe5f4fd238c370969776c7b0f997cc6d5`.
- **Candidate 3 — `v0.0.31-dsfix3`**: source `cb788a6005be2eb89e4981b578e9cacbc0c7bdb7`; native-library SHA-256 `124539effa99c355a5b3f32b2206e7705e5df6d5d9769b08d7d0450b37c18089`.
- **Candidate 4 — `v0.0.31-dsfix4`**: source `ce92ad84a6a86e4e04d39a52820e86ec01eebc95`; native-library SHA-256 `276f4f7c88fec9f2135de01ebbe50a75c45f35b6f7ef8db37398eddc4b33f055`.
- **Candidate 5 — `v0.0.31-dsfix5`**: source `7f061b24814b7938270c77ea8e44659f93044931`; native-library SHA-256 `0e793ff93d37f00f8dd3101654f02d44851c0cb3811c949f294775441a00eba3`.
- **Candidate 6 — `v0.0.31-dsfix6`**: source `119633325dc33d8fd633c3bfea71375ba43b245b`; native-library SHA-256 `200168f2dd888982b33353f23ae7faf46cff5bf4c5f784f84a0db13f15fd6249`.

- **Candidate 7 — `v0.0.31-dsfix7`**: source `90feb7a447f1f2602f5e9e5c1546e2fde8826f41`; native-library SHA-256 `14d0702d3600d93d9feff4c330ce3094d154f9376882b746152f8a39c1bb07b6`.
- **Candidate 8 — `v0.0.31-dsfix8`**: source `db22fcd8d833b661bcb975cd18cfdfd1d5cac0b4`; native-library SHA-256 `0442948bea5d1f78c72317591652952ae41a2517d0937ffd9ba371f420320778`.

- **Candidate 9 — `v0.0.31-dsfix9`**: source `b2811f5499114d389584ff2bec0de2afd5998481`; native-library SHA-256 `043ade578f8abae7966adf53e1552ac026604d937f66dd0a29e75390e0fb175c`.

- **Candidate 10 — `v0.0.31-dsfix10`**: source `99e1aa16ec04d432e4e06ff394df1bd781a381c5`; native-library SHA-256 `ae254d24b90faa694f0b710f57620b55290def6235e72a3b94bd0e5ff1f44fc0`.

- **Candidate 11 — `v0.0.31-dsfix11`**: source `23c453abf89235bfdfe2de3d343a7e8b43cf364c`; native-library SHA-256 `8f2729f76873b79dae160fff51413e92b8de5ec54688887b90b5745ae08f8b3a`.

- **Candidate 12 — `v0.0.31-dsfix12`**: source `a494bdc6909bdc0935fa0db2fbfb8406a44c929c`; native-library SHA-256 `074647dd11c4b5ee2405d54e82ab1cd3e27d1b362dec3f2a7ce305a02694188f`.

The selected Q4 row uses the separately labeled 16-CPU-allocation profile below; the other seven successful selected rows retain their historical four-CPU profiles.

The newer observations were read from saved artifacts `ds-fix7-q8-q7-q4-pilot` (Q7 validated, Q4 timeout and earlier Q8 worker OOM) and `ds-fix8-q8-pilot` (Q8 validated). Candidate 9 Q4, saved separately as `ds-fix9-q4-pilot`, still timed out during warm-up (SQLSTATE 57014, read-rows context deadline exceeded), with client OOM-kill delta zero, no Redis samples and no validated Redis output. Its native DuckDB control passed, median 0.046661375 seconds. Candidate 10 Q4 (`ds-fix10-q4-pilot`) also timed out during warm-up at the configured 60-second deadline (SQLSTATE 57014), with OOM-kill delta zero, no Redis timed samples and no validated Redis output; native DuckDB passed with median 0.048251417 seconds. A separate 16-CPU-allocation retry of the same native10 binary also failed as detailed below. Neither failed attempt establishes whole-query speedup or query resolution; the later Candidate 12 success is recorded separately below. Build identities match the separately retained build proofs; SQL SHA-256 values and all successful medians were independently checked from the saved outputs. The runner/isolation fingerprints and original source database hash match the earlier sequence. These are read-only evidence checks, not new executions.

The original source database SHA-256 was `508d4f5af490a4fb7645a9c682b507f8628b390960e5e9bd6f872b97a7d5760d`. SQL and source data were retained. Operational paths, runtime identifiers, endpoints, credentials and raw worker diagnostics are excluded.

## Separate 16-CPU-allocation Q4 follow-up

The original SF1 Q4 was retried alone using the **same** Candidate 10 source/version/native checksum, saved as `ds-fix10-16cpu-q4-pilot`. The verified client CPU quota was **16**, `GOMAXPROCS=16`, Redis driver query workers/budget **32/32**, client memory **8 GiB**, Software server ceiling **20 GiB**, and statement deadline **60 seconds**. Both DuckDB paths support the explicit client-thread profile; this attempt used native DuckDB and direct Redis only. Flex settings and verified active 4× QPF were retained.

Redis **timed out during warm-up** (SQLSTATE 57014), with client OOM-kill delta zero, no Redis timed samples, no median/ratio and no validated Redis output. The native DuckDB control passed, returning eight rows/four columns, median **0.055218083 seconds**. A diagnostic CPU snapshot was approximately **172%**, so this is a 16-CPU *allocation*, not evidence of full utilization of 16 cores. Neither the timeout nor that snapshot establishes scaling efficiency or a controlled cross-profile speedup.

This profile's saved harness fingerprints differ from the earlier four-CPU sequence: runner SHA-256 `6090ffb5372e6efd84fd6d04b11926af9e0444564ab6421e23f88f9daeac2d43`; isolation SHA-256 `56b05fbb2a23248d512c40acaa387dd953982598450c1c5cfe57e6d04e299c7f`. It uses explicit thread configuration/provenance added for the new profile; historical outputs are not relabeled. The original source database checksum is unchanged. No full suite was resumed.

### Candidate 11: owned-fact allocation follow-up

The same original Q4 was retried with Candidate 11 under the separate 16-CPU
allocation (`GOMAXPROCS=16`, query workers/budget 32/32, native DuckDB threads 16,
client 8 GiB, server 20 GiB, 60-second statement deadline, Flex and verified 4× QPF).
It timed out during warm-up, SQLSTATE 57014, with OOM-kill delta 0. There were no
Redis timed samples, median/ratio or validated output. Worker wall time including
cleanup was 60.534416653s; it is not a successful query latency. Native DuckDB
returned eight rows/four columns, with timed samples 0.071238332981 / 0.058321707998 /
0.047287583002s, median 0.058321707998s. A single diagnostic CPU snapshot 164.56%
does not establish 16-core utilization or scaling. These values do not replace
earlier profiles or establish Q4 resolution.

### Candidate 12: bounded chunk scheduling follow-up

The original SF1 Q4 completed under Candidate 12 with the separate 16-CPU
allocation: `GOMAXPROCS=16`, query workers/budget 32/32, native DuckDB threads 16,
client 8 GiB, server 20 GiB, 60-second statement deadline, Software Flex, verified
active 4× QPF and authenticated TLS. The Linux ARM64 client shared the server's
network namespace. The retained, backed-up data was used; no load was timed.

Redis warm-up was **38.604606184 s**. Timed runs were **43.049607144 /
43.873090352 / 45.976894605 s**, median **43.873090352 s**; all complete outputs
were independently validated at **eight rows/four columns**. The matched native
DuckDB median was **0.041623833 s**, so the within-attempt ratio is **1054.04×**.
This observation belongs to the exact combined source/artifact above, including
bounded chunk scheduling and earlier optimizer/allocation work. It does not
isolate any one PR, establish 16-core utilization or convert prior timeouts into
latency samples. Successful Q2/Q3/Q5/Q6/Q7/Q8/Q9 rows retain their historical
four-CPU profiles. All eight selected issue cases passed across multiple builds;
there was no single-build all-eight rerun or full 99-query benchmark.

## Source 13: separate three-path Q4 observation

This additional Q4 attempt preserves the Candidate 12 first-success row above;
it does not replace historical results or rerun all eight targeted queries on
one build. The same original SF1 SQL ran in sequential fresh processes with
separate disk-backed validation. The native DuckDB control and direct Redis
completed; the experimental DuckDB-over-Redis frontend did not reach validation.
See [frontend issue #627](https://github.com/redis/redis-adbc-driver/issues/627),
[allowlisted samples/provenance](tpcds-q4-three-path-2026-11-24.json) and
[upstream parallel-scanner PR #22](https://github.com/Query-farm/adbc_scanner/pull/22).

| Path | Warm-up (s) | Completed timed samples (s) | Median (s) | Outcome |
|---|---:|---|---:|---|
| Native DuckDB | 0.143135209 | 0.051986834 / 0.041131459 / 0.050459084 | 0.050459084 | Complete reference: 8 rows × 4 columns |
| Direct Redis ADBC | 39.169623976 | 40.448782644 / 39.139256434 / 48.450983522 | 40.448782644 | Warm-up and all timed outputs validated: 8 × 4 |
| DuckDB over Redis ADBC | 59.509124361 | None | — | Warm-up full fetch/export only; first timed repetition interrupted at 60 s |

**Frontend validation was not reached.** The worker fetches and exports each
repetition; independent validation starts only after all four repetitions finish.
The frontend's first timed repetition hit the actual DuckDB interrupt timer, with
`60-second statement deadline exceeded: INTERRUPT Error: Interrupted!`. Its
warm-up is a full-fetch observation only, not a correctness result. Temporary
result files were cleaned after failure, so no independent warm-up comparison is
claimed. There are **zero valid timed frontend samples, no median, no latency
ratio and no validated frontend row shape**. The record's eight-row/four-column
shape belongs to the native reference. Direct Redis was fully validated; its
within-attempt ratio to the matched native control is 801.62×. No frontend ratio
is inferred from its warm-up.

Source 13 driver: `v0.0.31-dsfix13`, source
`cc32226c3c37fc2b7f6c0fe188fc1b3d318ad69f`, library SHA-256
`22d7aac800fd653e2e94e35842b2612c818ce7a96299c230f0662182ed60e168`.
The experimental scanner executed local source
`12a2857ded755ed9f0fe3d88b7dcd96ea8012903`; public fork head
`c29122e13b4eec8db12b7dbd44bff845582c722e` has the identical whole-tree identity
`fdec3b2031b358f47eadb420320a85d923f1054f`. Scanner binary SHA-256:
`0764fe5e3f0d356b3a6d049e778fff577ec519d99912271fb5fb4bcf73ec025c`.
This is an experimental source build, not a released-scanner comparison or a
claim that the public commit itself was rebuilt for this run.

The separate profile allocated 16 client CPUs, `GOMAXPROCS=16`, Redis query
workers/budget 32/32, database `adbc.redis.partition_count=16`, DuckDB threads 16,
requested scanner `scan_threads=16`, client memory 8 GiB and Software server
ceiling 20 GiB, with a 60-second statement deadline. Redis Software Flex,
verified active 4× QPF and authenticated TLS were retained; the Linux ARM64
client shared the server's network namespace. Whole-query remote pushdown was
disabled, so DuckDB planned joins/aggregates over Redis-backed table scans.
[Driver PR #625](https://github.com/redis/redis-adbc-driver/pull/625) supplies the
database/connection partition-count default. Configured settings alone do not
establish CPU utilization or actual concurrency of each Q4 source scan.

A separate live API correctness probe returned 16 partitions, opened 16 readers
simultaneously, and verified all 100,000 exact customer IDs. It is **not** a Q4
concurrency trace, CPU-scaling measurement or query performance result. No new
loading was timed. The retained compatible SF1 data and SQL were unchanged.
The frontend timeout is tracked separately from direct-path #590; this report
requests no issue closure and assigns no bottleneck to one layer without further
evidence. Full benchmark suites remain stopped.

## Common recorded profile and validation

Redis Software (Redis engine 8.8.0), Flex experimental stopgap with active, verified 4× QPF, six Search workers, mnp routing and 32 connections. Logical database capacity 64 GiB; Flex RAM cache 8 GiB. Linux ARM64 query client in the server network namespace, four client CPUs with an 8 GiB client ceiling for the original sequence (the latest Q4 16-CPU allocation is labeled separately above); Software container ceiling 20 GiB. The complete retained SF1 fixture was count-verified (24 tables, 19,557,375 rows) and backed up before this focused sequence. No loading-performance result is implied.

Each successful engine used one warm-up and three timed runs, full result fetch and independent multiset validation. Numeric comparison retained the existing relative tolerance 1e-7 / absolute tolerance 1e-6; ordering and exact cross-engine type identity were not separately scored. Native DuckDB and direct Redis ADBC ran in sequential fresh processes, with separate disk-backed validation. No DuckDB-over-Redis third path was used for these TPC-DS attempts.

### Provenance verified after the runs

The original focused-run metadata embedded the native driver identity and Linux/ARM64 route, but did not capture the installed DuckDB/PyArrow versions or host hardware model before timing. Subsequent read-only inspection verified that the baseline DS SDK, Candidate 2 and Candidate 6 clients reference the **same immutable client image**. A separate network-disabled, metadata-only process from that image, limited to 256 MiB, reported **DuckDB 2.0.1** and **PyArrow 26.0.1**. The image identifier and operational verification record remain private. This image verification is scoped to those three clients; it is not extended to other candidates without their own evidence.

After-run host inspection reported **Apple M5 Max, 18 logical CPUs and 38,654,705,664 bytes (36 GiB) RAM**. These describe the physical host. The timed query client remained limited to **four CPUs / 8 GiB**, and the Software server container ceiling was **20 GiB**, with an **8 GiB Flex RAM cache**. The 256 MiB metadata-verification process was separate from the timed query client. Container ceilings are not measurements of driver peak memory.

These are explicitly **after-run provenance checks**, not a claim that the fields were captured or frozen before timing. Shared image identity does not establish controlled cross-build speedup or isolate optimizer changes, resource contention or cache effects.

## Failure and attribution context

- The isolated baseline timed out on Q2/Q3/Q9. Q5/Q7/Q8 had worker OOMs; Q4 had client cgroup OOM with coordinator loss and a recovered terminal record. That Q4 observation was not a clean worker-only OOM.
- Candidate 1 still timed out on Q3 and OOMed Q4/Q5. Candidate 2 validated Q3 while Q7/Q8 timed out. Candidate 3 still timed out on Q9. Candidate 4 OOMed Q4. Candidate 5 later timed out on Q4/Q8 without an OOM-kill counter increase. Candidate 6 validated Q2/Q5/Q9 while Q7 still timed out without an OOM-kill increase. Candidate 7 validated Q7; Q4 still timed out at its configured 60-second statement deadline (SQLSTATE 57014, OOM-kill delta zero), while Q8 had a worker OOM (exit -9, OOM-kill delta one). Candidate 8 then validated Q8 with OOM-kill delta zero. Candidates 9 and 10 again timed out on Q4 without a client OOM-kill increase; no query resolution or latency follows from that failed attempt. The Candidate 7 Q8 failure is preserved as history and supplies no Redis median. No failed attempt is treated as a latency sample.
- Earlier shared-worker historical OOM observations and the later isolated attempts have different process/resource conditions. A change in failure category or latency cannot be assigned solely to driver code, a single PR, memory reduction or cache state.
- The native6 combined source included cohort, LEFT-join, fact-demand, Flex-cap and streaming work. The successful observations do not isolate those changes. [PR #610](https://github.com/redis/redis-adbc-driver/pull/610) additionally hardens the separately discovered synthetic HASH pipeline uncertainty in [#609](https://github.com/redis/redis-adbc-driver/issues/609); that stricter guard postdates the recorded native6 binary. There was no reported ambiguous transport failure in the validated Q9 attempt.
- Candidate 7 additionally combined bounded parallel fact reading with fact-demand HASH-list fixes, native join ordering and cohort work; Candidate 8 added further native join-order/key-constraint corrections. The validated Q7/Q8 observations belong to those exact combined artifacts and do not establish isolated performance for [PR #612](https://github.com/redis/redis-adbc-driver/pull/612), its public head or any individual optimizer change.
- The report requests no automatic query-issue closures. All eight targeted cases have a validated observation across combined artifacts; this does not establish that every query passes on one current public build, or that the unrun suite remainder passes.
