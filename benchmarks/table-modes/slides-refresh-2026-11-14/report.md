# Performance-slide benchmark refresh — November 14, 2026

All **324 trials validated** on clean merged main `34f4c82972c7a059f045f1fb65bdfc2ce4adfce9`: 156 bulk/read/creation records and 168 OLTP/OLAP records. There are 72 warmups and 252 measured trials. No failed trial, serialization conflict or discarded sample. This is a fresh current-build reference on native Linux, not a matched speedup comparison against the older macOS results. No ClickBench or other product benchmark was rerun.

The same exact native library and CLI served both matrices. `adbc.redis.writer_admission_queue` remained at its **default false**. The earlier opt-in writer-queue comparison is a separate experiment; its configuration and results are not substituted here.

## What the measurements show

- Managed mode leads the wide scan and the 16-group COUNT/SUM query in these fixtures.
- Redis-py leads point reads/mutations and the narrow filtered projection. Managed filtered COUNT/SUM is close to the Redis-client reducer path by median; the raw observations remain visible.
- One-writer unmanaged Arrow append is close to HSET by median. SQL ingestion and managed publication take longer. Four managed callers serialize admitted mutations.
- Unmanaged create-and-fill and the Redis-client baseline are close in the one-writer median; these methods create different indexes and provide different guarantees.

Finite samples and overlapping ranges do not establish statistical equivalence. Use the numeric tables and full distributions, not these statements as a product-wide ranking.

## Point operations

1,000 rows × four typed columns; one known record per call. Persistent per-thread client/cursor objects, one or four clients. Each measured cell contains 96 operation observations (three rounds ×32), after a separate warmup trial. P50/p95 pool the measured operation latencies; throughput is the median of closed-loop trial throughput, not saturated capacity.

| Operation | Mode | Clients | P50 ms | P95 ms | Median ops/s | Samples |
|---|---|---|---|---|---|---|
| delete | managed | 1 | 6.103 | 31.653 | 79.492 | 96 |
| delete | managed | 4 | 8.866 | 175.908 | 79.038 | 96 |
| delete | redis_py | 1 | 0.043 | 0.158 | 8717.071 | 96 |
| delete | redis_py | 4 | 0.139 | 4.087 | 6157.843 | 96 |
| delete | unmanaged | 1 | 2.212 | 3.921 | 416.048 | 96 |
| delete | unmanaged | 4 | 5.882 | 11.481 | 602.643 | 96 |
| insert | managed | 1 | 5.828 | 42.723 | 82.644 | 96 |
| insert | managed | 4 | 7.007 | 280.214 | 68.713 | 96 |
| insert | redis_py | 1 | 0.046 | 0.165 | 8163.353 | 96 |
| insert | redis_py | 4 | 0.240 | 4.149 | 5676.988 | 96 |
| insert | unmanaged | 1 | 2.296 | 3.666 | 413.515 | 96 |
| insert | unmanaged | 4 | 5.749 | 9.561 | 646.855 | 96 |
| point_read | managed | 1 | 2.012 | 3.082 | 465.070 | 96 |
| point_read | managed | 4 | 4.943 | 10.335 | 722.736 | 96 |
| point_read | redis_py | 1 | 0.054 | 0.109 | 7849.790 | 96 |
| point_read | redis_py | 4 | 0.380 | 4.550 | 4501.468 | 96 |
| point_read | unmanaged | 1 | 2.008 | 2.927 | 470.999 | 96 |
| point_read | unmanaged | 4 | 4.899 | 8.275 | 731.289 | 96 |
| update | managed | 1 | 6.388 | 31.509 | 81.508 | 96 |
| update | managed | 4 | 7.486 | 285.766 | 67.421 | 96 |
| update | redis_py | 1 | 0.046 | 0.178 | 8383.363 | 96 |
| update | redis_py | 4 | 0.182 | 4.827 | 4813.568 | 96 |
| update | unmanaged | 1 | 2.259 | 3.397 | 407.991 | 96 |
| update | unmanaged | 4 | 6.147 | 11.043 | 608.057 | 96 |

Every read completely fetches the typed Arrow result; point mutations independently validate the complete final fixture. Managed addressing uses known internal row IDs; unmanaged/raw addressing uses application HASH keys. Lookup is excluded. Managed snapshot publication and unmanaged conditional INSERT differ from raw HSET; no multi-statement transaction claim follows.

## Wide reads

100,000 rows ×105 columns. One warmup and six measured repetitions per cell. Four readers execute eight disjoint ranges using separate statements, without a shared cross-statement snapshot.

| Mode | Readers | Batch ceiling | Median s | Min–max s | First batch s | Max-worker RSS MiB |
|---|---|---|---|---|---|---|
| managed | 1 | 512 | 1.371 | 1.316–1.449 | 0.201 | 761.953 |
| managed | 1 | 4096 | 1.335 | 1.282–1.361 | 0.198 | 761.953 |
| managed | 4 | 512 | 1.170 | 1.131–1.208 | 0.325 | 761.953 |
| managed | 4 | 4096 | 1.119 | 1.082–1.145 | 0.317 | 761.953 |
| unmanaged | 1 | 512 | 4.708 | 4.664–4.802 | 0.206 | 761.953 |
| unmanaged | 1 | 4096 | 4.445 | 4.430–4.464 | 0.364 | 761.953 |
| unmanaged | 4 | 512 | 1.535 | 1.504–1.566 | 0.343 | 761.953 |
| unmanaged | 4 | 4096 | 1.448 | 1.430–1.542 | 0.342 | 761.953 |
| redis_py | 1 | 512 | 4.198 | 4.123–4.240 | 0.245 | 761.953 |
| redis_py | 1 | 4096 | 4.002 | 3.981–4.181 | 0.271 | 761.953 |
| redis_py | 4 | 512 | 1.863 | 1.769–1.957 | 0.359 | 761.953 |
| redis_py | 4 | 4096 | 1.841 | 1.674–1.889 | 0.386 | 761.953 |

## Existing-table bulk ingestion

10,000 total rows ×105 public columns plus an ordinal; up to 1,000 rows per call/pipeline, divided across one or four writers. One warmup plus three measured repetitions.

| Mode | API | Operation | Writers | Median s | Min–max s | Max-worker RSS MiB |
|---|---|---|---|---|---|---|
| managed | sql | append | 1 | 2.760 | 2.731–2.797 | 777.715 |
| managed | sql | append | 4 | 4.520 | 4.359–4.643 | 777.715 |
| unmanaged | sql | append | 1 | 1.809 | 1.761–1.815 | 777.715 |
| unmanaged | sql | append | 4 | 1.025 | 1.017–1.043 | 777.715 |
| managed | arrow | append | 1 | 2.331 | 2.319–2.411 | 777.715 |
| managed | arrow | append | 4 | 4.252 | 4.230–4.254 | 777.715 |
| unmanaged | arrow | append | 1 | 1.035 | 1.026–1.040 | 777.715 |
| unmanaged | arrow | append | 4 | 0.827 | 0.827–0.829 | 777.715 |
| redis_py | hset | append | 1 | 1.064 | 1.060–1.068 | 777.715 |
| redis_py | hset | append | 4 | 0.757 | 0.733–0.803 | 777.715 |

## Create and fill

10,000 total rows ×105 public columns plus an ordinal; up to 1,000 rows per call/pipeline, divided across one or four writers. One warmup plus three measured repetitions.

| Mode | API | Operation | Writers | Median s | Min–max s | Max-worker RSS MiB |
|---|---|---|---|---|---|---|
| managed | arrow | create | 1 | 2.336 | 2.314–2.355 | 777.715 |
| unmanaged | arrow | create | 1 | 0.650 | 0.631–0.668 | 777.715 |
| managed | arrow | create_append | 1 | 2.346 | 2.344–2.382 | 777.715 |
| managed | arrow | create_append | 4 | 4.263 | 4.262–4.434 | 777.715 |
| unmanaged | arrow | create_append | 1 | 0.638 | 0.627–0.645 | 777.715 |
| unmanaged | arrow | create_append | 4 | 0.438 | 0.437–0.444 | 777.715 |
| redis_py | hset | create | 1 | 0.689 | 0.687–0.689 | 777.715 |
| redis_py | hset | create | 4 | 0.392 | 0.384–0.393 | 777.715 |

Existing-table preparation is excluded from ingestion timing. Create-and-fill includes table/index work: unmanaged/raw creation has membership-only indexes; managed creation maintains its private indexed snapshot layout. Schema/catalog initialization remains outside. HSET does not implement the driver's whole-key absence checks or snapshot publication.

## Filters and aggregates

100,000 rows ×four typed columns; the projection returns 100 rows, filtered COUNT/SUM matches 10,000 rows and grouped COUNT/SUM returns 16 groups. Each measured cell has 24 query observations (three rounds ×eight), after warmup.

| Query | Mode | Clients | P50 ms | P95 ms | Median queries/s | Samples |
|---|---|---|---|---|---|---|
| filtered_count_sum | managed | 1 | 3.343 | 6.231 | 279.983 | 24 |
| filtered_count_sum | managed | 4 | 7.775 | 10.535 | 505.728 | 24 |
| filtered_count_sum | redis_py | 1 | 2.789 | 5.737 | 322.033 | 24 |
| filtered_count_sum | redis_py | 4 | 8.063 | 16.195 | 399.993 | 24 |
| filtered_count_sum | unmanaged | 1 | 30.057 | 32.718 | 32.855 | 24 |
| filtered_count_sum | unmanaged | 4 | 50.216 | 54.410 | 77.316 | 24 |
| filtered_projection | managed | 1 | 2.780 | 5.567 | 325.847 | 24 |
| filtered_projection | managed | 4 | 7.467 | 10.552 | 479.777 | 24 |
| filtered_projection | redis_py | 1 | 0.796 | 3.100 | 935.622 | 24 |
| filtered_projection | redis_py | 4 | 3.451 | 7.131 | 959.583 | 24 |
| filtered_projection | unmanaged | 1 | 2.645 | 5.505 | 343.086 | 24 |
| filtered_projection | unmanaged | 4 | 7.303 | 10.471 | 538.067 | 24 |
| grouped_count_sum | managed | 1 | 11.761 | 13.727 | 81.597 | 24 |
| grouped_count_sum | managed | 4 | 16.743 | 21.256 | 229.952 | 24 |
| grouped_count_sum | redis_py | 1 | 35.327 | 38.590 | 28.048 | 24 |
| grouped_count_sum | redis_py | 4 | 102.765 | 181.056 | 37.013 | 24 |
| grouped_count_sum | unmanaged | 1 | 269.200 | 278.686 | 3.693 | 24 |
| grouped_count_sum | unmanaged | 4 | 441.349 | 495.486 | 8.486 | 24 |

SQL COUNT/SUM outputs are cast to BIGINT for matching int64 Arrow types. The raw client uses Search cursor discovery plus HMGET for projection, and native Search reducers for aggregate cases, including typed Arrow conversion. Equivalent results do not imply identical execution or consistency semantics. Synthetic data have no NULLs/floating-point edge cases; this fixture does not prove reducers safe for arbitrary int64 sums or distributions.

## Chart data and slides

[charts.json](charts.json) and [charts.csv](charts.csv) contain 84 current chart rows: point reads/mutations, bulk SQL/Arrow append, both scan ceilings, filter/aggregate queries, data movement and the numeric slide-4 overview. Ratios are driver elapsed / redis-py elapsed; below 1× means less elapsed time in that case.

Slides 4 and 19–22 use the current values; slide 25 synthesizes the current results. Historical ClickBench slides 23–24 retain their original numbers and labels. [Historical product CSV](clickbench-historical.csv) and [historical aggregate CSV](aggregate-historical.csv) are regenerated offline from their unchanged archived inputs, never timed or mixed into the current Software series. Their unequal cross-product hardware and different hot-minimum/median metrics remain explicit.

## Environment and timing boundaries

Driver source `34f4c82972c7a059f045f1fb65bdfc2ce4adfce9`, Makefile version `v0.1.0-dev.slide-refresh-20261114`. Linux/arm64 native Python 3.14.10, Go 1.27.3, PyArrow 26.0.1, adbc-driver-manager 1.13.0, redis-py 8.1.0, hiredis 3.5.0. Client allocation: four CPUs/4 GiB. Client shares the Software server's network namespace and uses localhost with certificate/hostname-verified TLS and RESP3.

Redis Software 8.2.6-25 / Redis 8.8.0 / Search 80808, one 4-GiB RAM shard, active/effective 4× QPF, six Search workers, mnp routing/32 connections, noeviction. Actual server limits: four CPUs/8 GiB. Host: Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3; Docker VM 18 CPUs. Fixed recorded cached server image throughout, not registry-digest-pinned. Exact build/input/harness identities and resource observations are in [provenance.json](provenance.json). GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG were not explicitly set for the native driver.

The earlier macOS/published-port transport anomaly was absent in separate Linux GET/PING controls. This run uses that Linux topology, but does not isolate OS from route/runtime/resource changes or attribute the difference from older slide numbers solely to code changes. Older observations retain their original labels. No Docker component or server CPU cause is established by this rerun.

Bulk/scan timing includes fresh spawned worker startup/imports, source loading/conversion, connections, full reads/writes, Arrow IPC output and shutdown. Fixture setup and independent typed-value/count/multiplicity checks are outside timing. RSS is the maximum individual worker lifetime high-water mark including Python/source/native buffers, not summed memory or Go heap.

Scenario timing includes SQL execute/full fetch or acknowledged Redis commands plus typed conversion. Client/cursor object creation, fixture preparation, independent validation and teardown are outside timing. Lazy first-operation transport work remains included; each trial uses fresh clients, so the warmup does not preconnect later trials' pools. The original `automatic_retries=0` metadata describes the harness's no-caller-replay policy; it is not an instrumented wire retry/reconnect counter.

Bulk and scenario suites ran consecutively; case order alternates within each suite. Heavy build/test/render work was paused during timing. These finite samples, scheduling and cache effects do not establish confidence intervals, production capacity or SLA tails.

## Reproduction, validation and cleanup

Provision an owned fixture with `make -C go redis-up`, configure and verify the effective one-shard/4-GiB/4×QPF settings and limits above, and build the exact clean source with `make -C go build adbc-cli VERSION=v0.1.0-dev.slide-refresh-20261114` inside the [Linux benchmark client](../../linux-client/README.md). Mount the Git common directory and supply credentials/trusted CA privately.

Run [arrow_compare.py](../arrow_compare.py) with suite `all`, six measured read rounds, three measured write rounds, 10,000 write rows and 1,000-row calls. Use the canonical 100K×105 typed Arrow input identified in provenance. After all clients exit, verify the run's ownership marker and empty only that owned database. Run [scenarios.py](../scenarios.py) with suite `all`, three measured rounds, 32 point operations, eight queries, 1,000 point rows and 100,000 analytical rows. Use the same unchanged library/CLI and a fresh work directory. The external wrapper only recorded bulk ownership outside timing; the measured timer and oracle bodies were unmodified.

Run `python3 benchmarks/table-modes/slides-refresh-2026-11-14/audit.py` for the complete matrix, independent arithmetic, exact chart regeneration, source/harness hashes and cleanup proof. All owned clients ended, owner markers matched before cleanup, the database was verified empty/index-free with TLS/RESP3, and the owned server/network/client tag and generated TLS/environment/credential files were removed. The initial container inventory was empty; the unowned network ID/configuration inventory captured after setup was preserved. Base images/build caches and private operational logs remain. See [cleanup.json](cleanup.json).
