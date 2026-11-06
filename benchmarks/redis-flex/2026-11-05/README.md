# Redis Flex stopgap comparisons

Date: 2026-11-05. Driver: PR #323, based on 689dbc6, with the profile harness added in this change. Measurements precede the later validation-driven error, retained-mode and allocation-bound fixes; the measured read algorithms were unchanged. These are warm synthetic local measurements, not ClickBench or production sizing evidence.

This is the historical **1,000-ID bounded-page baseline**, merged in
[PR #323](https://github.com/redis/redis-adbc-driver/pull/323). The
[latest follow-up](followups/README.md), merged in
[PR #331](https://github.com/redis/redis-adbc-driver/pull/331), measures 10,000-ID
pages and tiered/concurrent reads. In its latest matched warm results, Flex
stopgap averaged **6.9x RAM-default latency** and RAM stopgap **3.1x** across
seven workloads, using ratios of mean per-workload medians. The tables below
retain the earlier baseline and must not be read as those latest results.

## Method

- Redis Software 8.2.6-25, Redis 8.8.0, Search 8.8.8; Linux aarch64 containers on one macOS ARM host. RAM and Flex each had one shard, verified active 4x QPF, 6 Search workers, mnp routing and 32 connections. Both used authenticated TLS and RESP3. Flex had BigStore v2, 1 GiB total / 256 MiB RAM; RAM had 1 GiB, no replication. Flash was a container directory, not a provisioned SSD service.
- Same 50,000 generated rows: id 1..50000; g=id%257; n=(id*7919)%1000003, NULL when id%17=0. Twelve unindexed string columns each contain the row ID plus a repeated 120-byte suffix. Numeric fields and hidden row IDs are indexed. This compressible dataset is deliberately below the Flex RAM allocation; it does not measure a cold, flash-dominant working set.
- One worker and worker budget 1, GOMAXPROCS=18, Go 1.27.3. Docker reported 18 CPUs and 25,421,033,472 bytes of VM memory. One warmup and five measured repetitions per configuration/workload, rotating configuration order. No other validation suites ran during timed trials. The shared host and existing idle containers were not CPU-pinned.
- SQL timings include ExecuteQuery, full Arrow consumption and checking every returned field against independent formulas. Reader timings include exact HASH fetches and full row validation. Results are medians; five repetitions do not establish a reliable production p95.

## End-to-end SQL latency

| Workload | RAM default, ms | RAM stopgap, ms | Stopgap/default | Flex stopgap, ms |
|---|---:|---:|---:|---:|
| selective | 17.64 | 9.67 | 0.55x | 13.33 |
| broad | 334.74 | 2826.02 | 8.44x | 4640.73 |
| wide | 652.26 | 2947.39 | 4.52x | 5194.36 |
| topk | 24.12 | 2519.22 | 104.44x | 4658.28 |
| sort | 376.94 | 2527.60 | 6.71x | 4688.18 |
| scalar | 242.17 | 2387.32 | 9.86x | 4558.73 |
| grouped | 283.77 | 2509.89 | 8.84x | 4606.50 |

Selective is the final 100 IDs, ordered by ID. Broad projects id,n; wide projects id plus 12 strings. Top-K sorts n NULLS LAST,id and returns 100 rows; full sort has no limit. Scalar computes COUNT(*), SUM(id), MIN(n), MAX(n). Grouped computes COUNT(*) and SUM(id) for 257 groups ordered by g.

The RAM comparison includes actual execution choices: default can use sortable pipelines and cursors; the stopgap evaluates ordering and aggregates in the client. It measures the complete feature tradeoff, not just paging. The existing default path can itself choose bounded aggregate-cursor windows. Selective differences should not be generalized beyond this fixture.

## Isolating schema and reader changes on RAM

Raw scans use both schemas with both readers, identical filters, and the same exact store.fetchRows HASH conversion. Neither uses server sorting or reducers. Cursor pages contain at most 10,000 keys; bounded requests ask for at most 1,000. Reader/page-size differences are included as implemented.

| Workload | Sortable/cursor, ms | Plain/cursor, ms | Sortable/bounded, ms | Plain/bounded, ms |
|---|---:|---:|---:|---:|
| selective | 11.77 | 9.76 | 2.07 | 4.63 |
| broad | 758.23 | 756.05 | 2504.53 | 2481.90 |
| wide | 1018.52 | 1042.92 | 2602.15 | 2554.40 |

FT.INFO reports **13.89 MiB sortable versus 6.21 MiB plain** total index memory, a **55.3% reduction** for this schema. Sortable-value storage fell from 7.54 MiB to zero. These figures are index memory, not total server/flash allocation. Both RAM indexes coexisted, so process RSS cannot isolate their individual storage cost.

Removing SORTABLE had little effect on broad raw-scan latency here. Bounded broad and wide scans were much slower than cursors: disjoint splitting generated 127 Search requests rather than approximately five cursor pages. HASH commands were pipelined in both cases. Selective lookup did not require a full-domain split.

## Resources (SQL medians)

| Target/path | Workload | Redis commands | RESP read+write, MiB | Client CPU, s | Allocated, MiB | Sampled Go heap peak, MiB | Redis+proxy CPU, s |
|---|---|---:|---:|---:|---:|---:|---:|
| ram/default | broad | 50036 | 12.73 | 0.155 | 105.3 | 39.0 | 0.18 |
| ram/default | wide | 36 | 90.59 | 0.439 | 452.5 | 335.8 | 0.50 |
| ram/default | topk | 108 | 0.03 | 0.004 | 1.0 | 4.4 | 0.01 |
| ram/default | grouped | 50016 | 11.42 | 0.179 | 134.9 | 30.0 | 0.13 |
| ram/bounded | broad | 50137 | 14.35 | 0.386 | 111.3 | 18.7 | 0.21 |
| ram/bounded | wide | 50137 | 94.79 | 0.658 | 458.3 | 305.0 | 0.42 |
| ram/bounded | topk | 50137 | 14.35 | 0.452 | 166.8 | 10.1 | 0.21 |
| ram/bounded | grouped | 50137 | 14.22 | 0.432 | 145.1 | 10.5 | 0.20 |
| flex/bounded | broad | 50137 | 14.04 | 0.465 | 111.1 | 18.1 | 6.21 |
| flex/bounded | wide | 50137 | 94.48 | 0.771 | 458.2 | 304.8 | 6.46 |
| flex/bounded | topk | 50137 | 14.04 | 0.513 | 166.8 | 9.5 | 6.15 |
| flex/bounded | grouped | 50137 | 13.91 | 0.491 | 145.1 | 9.8 | 6.21 |

Command counts include every HMGET in a pipeline; they are not network round trips. RESP bytes include metadata and connection initialization, exclude TLS framing, and cover the full result fetch. Allocations include validation. Go heap was sampled every 1 ms, includes the generated validation oracle and process baseline, and can miss short peaks. It is not isolated driver-only memory. Process lifetime peak RSS includes fixture ingestion and is retained only in samples.jsonl; it is not a per-query peak.

Server CPU is the delta of Linux /proc counters for all redis-server processes plus dmcproxy in the dedicated container at 10 ms resolution, including background work. INFO CPU returned placeholder zeros and was not used. Server RSS is measured before/after each query, not as a transient peak. Per-process /proc/io block-read/write deltas are in the samples; these do not count document/flash reads. All measured Flex /proc/io block-read/write deltas were zero. This small warm working set cannot establish production flash amplification. No general external-sort spill path was exercised. Aggregate spill counters, when present, are preserved only for SQL records; a missing counter is not evidence of zero unobserved I/O.

## Reproduce

Provision isolated RAM and Flex fixtures with repository setup scripts, authenticated TLS and verified 4x QPF. Select one shard for each. From go/, source the relevant generated test environment. For Flex set REDIS_URI="$REDIS_FLEX_URI". Use fresh private output directories:

```sh
REDIS_FLEX_BENCH_TARGET=ram \
REDIS_FLEX_BENCH_CONTAINER=<owned-ram-container> \
REDIS_FLEX_BENCH_DIR=<fresh-private-output-directory> \
REDIS_FLEX_BENCH_ROWS=50000 REDIS_FLEX_BENCH_TRIALS=5 \
go test -count=1 -p 1 -timeout 30m -run '^TestFlexComparativeProfile$' -v .
```

For Flex use TARGET=flex and its owned container. The harness requires Docker access for server process counters, creates only its synthetic tables, verifies all rows, and drops its tables at completion. Never point it at production.

The 180 measured samples are in [samples.jsonl](samples.jsonl). Grouped medians/ranges and resource counters are in [summary.json](summary.json). Fixture and index metadata omit credentials, hostnames, endpoints, cloud IDs and operational control records.

## Implication

The stopgap enables restricted Flex queries and saves index memory, but is not a performance replacement for sortable/cursor execution. Broad reads pay repeated Search overhead; top-K loses the server-side shortcut and scans all matches. Keep the mode opt-in. Larger/cold datasets, concurrency, SSD behavior, remote latency, independent page-size tuning, and Cloud performance need separate experiments.

## Software Flex follow-up

The [follow-up measurements](followups/README.md) retain this historical baseline and add a matched before/after page-size comparison, sampled server RSS peaks, a flash-dominant working set, and 1/2/4-reader throughput and batch-memory measurements.
