# Local QPF benchmark

Measured on October 6, 2026 using a dedicated disposable Redis Software container.

## Method

- Redis Software image: `redislabs/redis:8.2.0-78.18`; database Redis 8.6.2, Search 8.6.11.
- One shard, no replica, RAM storage, 1 GiB database limit, 100,000 deterministic hashes with a 512-byte payload.
- Docker host: 10 virtual CPUs, approximately 7.75 GiB RAM, ARM64.
- Actual QPF API settings: disabled/standard, 2×, and 4×; API readback verified after every update.
- Six direct Redis command workloads, concurrency 1 and 16, three 3-second trials per combination.
- QPF order rotated across rounds: standard/2×/4×, 4×/standard/2×, 2×/4×/standard.
- Each worker has a separate connection, warms up before a synchronized start, and sends one request at a time.
- Throughput counts completed requests over actual elapsed time, including in-flight requests finishing after the deadline.
- Identical query results checked before each configuration; both aggregation variants produce the same sums.
- Tables show median trial throughput and median trial p95; p95 is not pooled across trials.

## Limits

This is a shared local Docker host, not a Redis Cloud deployment or an isolated capacity test. During execution another Redis test container consumed about 337% CPU while this container consumed about 518%. Background load varied; throughput ranges below expose run-to-run variability. Do not interpret differences as a pure causal estimate of QPF or guaranteed production speedups. Some slow aggregation trials complete few requests, so their p95 estimates are coarse. Python client parsing and thread scheduling can also limit large-result throughput. This measures direct FT commands, not end-to-end ADBC SQL/Arrow performance. No cloud or existing application databases were changed.

## Concurrency 1

| Workload | QPF | Median QPS | QPS range | Median p95 ms | QPS vs standard | Trials |
|---|---:|---:|---:|---:|---:|---:|
| search_text_count | standard | 205.9 | 72.3–344.5 | 8.72 | 1.00× | 3 |
| search_text_count | 2 | 159.2 | 13.0–345.6 | 18.20 | 0.77× | 3 |
| search_text_count | 4 | 140.7 | 93.2–227.3 | 17.88 | 0.68× | 3 |
| search_tag_projection | standard | 368.9 | 117.9–901.3 | 10.11 | 1.00× | 3 |
| search_tag_projection | 2 | 155.0 | 8.0–941.6 | 30.32 | 0.42× | 3 |
| search_tag_projection | 4 | 142.9 | 66.9–153.5 | 20.77 | 0.39× | 3 |
| search_full_document | standard | 59.9 | 59.6–81.0 | 20.51 | 1.00× | 3 |
| search_full_document | 2 | 19.0 | 15.6–60.4 | 216.20 | 0.32× | 3 |
| search_full_document | 4 | 86.2 | 43.2–90.1 | 26.14 | 1.44× | 3 |
| aggregate_sortable | standard | 164.3 | 61.5–220.3 | 8.50 | 1.00× | 3 |
| aggregate_sortable | 2 | 73.3 | 35.3–258.0 | 28.39 | 0.45× | 3 |
| aggregate_sortable | 4 | 88.9 | 66.3–100.0 | 21.45 | 0.54× | 3 |
| aggregate_load | standard | 25.2 | 15.6–27.6 | 55.89 | 1.00× | 3 |
| aggregate_load | 2 | 10.2 | 5.2–32.5 | 512.70 | 0.41× | 3 |
| aggregate_load | 4 | 10.0 | 3.9–15.7 | 223.83 | 0.40× | 3 |
| search_numeric_count | standard | 910.3 | 264.6–1,786.5 | 1.95 | 1.00× | 3 |
| search_numeric_count | 2 | 350.7 | 332.3–2,144.3 | 11.03 | 0.39× | 3 |
| search_numeric_count | 4 | 1,125.6 | 518.8–1,217.0 | 1.81 | 1.24× | 3 |

## Concurrency 16

| Workload | QPF | Median QPS | QPS range | Median p95 ms | QPS vs standard | Trials |
|---|---:|---:|---:|---:|---:|---:|
| search_text_count | standard | 931.6 | 344.1–1,447.6 | 31.01 | 1.00× | 3 |
| search_text_count | 2 | 587.2 | 312.1–1,346.2 | 50.83 | 0.63× | 3 |
| search_text_count | 4 | 838.0 | 692.3–875.2 | 42.47 | 0.90× | 3 |
| search_tag_projection | standard | 2,027.9 | 904.2–2,176.3 | 16.07 | 1.00× | 3 |
| search_tag_projection | 2 | 1,712.9 | 1,329.7–3,338.5 | 25.75 | 0.84× | 3 |
| search_tag_projection | 4 | 1,644.7 | 1,388.0–2,420.3 | 21.08 | 0.81× | 3 |
| search_full_document | standard | 752.4 | 622.8–821.7 | 40.68 | 1.00× | 3 |
| search_full_document | 2 | 802.4 | 154.6–909.7 | 35.99 | 1.07× | 3 |
| search_full_document | 4 | 647.7 | 193.4–829.4 | 52.54 | 0.86× | 3 |
| aggregate_sortable | standard | 429.5 | 200.8–644.4 | 81.36 | 1.00× | 3 |
| aggregate_sortable | 2 | 424.3 | 53.6–515.1 | 62.11 | 0.99× | 3 |
| aggregate_sortable | 4 | 499.8 | 292.4–589.2 | 68.55 | 1.16× | 3 |
| aggregate_load | standard | 29.3 | 26.0–30.9 | 759.40 | 1.00× | 3 |
| aggregate_load | 2 | 15.4 | 1.7–20.2 | 1466.14 | 0.53× | 3 |
| aggregate_load | 4 | 28.3 | 6.1–37.0 | 736.05 | 0.97× | 3 |
| search_numeric_count | standard | 5,829.4 | 5,534.1–7,057.0 | 4.40 | 1.00× | 3 |
| search_numeric_count | 2 | 3,543.9 | 793.0–4,277.1 | 12.40 | 0.61× | 3 |
| search_numeric_count | 4 | 4,763.4 | 1,793.0–8,879.1 | 6.48 | 0.82× | 3 |

## Exact queries

### search_text_count

```text
FT.SEARCH qpf_idx alpha|beta NOCONTENT LIMIT 0 0 DIALECT 2
```

### search_tag_projection

```text
FT.SEARCH qpf_idx @country:{US} SORTBY id ASC RETURN 2 id amount LIMIT 0 10 DIALECT 2
```

### search_full_document

```text
FT.SEARCH qpf_idx @country:{US} SORTBY id ASC LIMIT 0 100 DIALECT 2
```

### aggregate_sortable

```text
FT.AGGREGATE qpf_idx * GROUPBY 1 @country REDUCE SUM 1 @amount AS total SORTBY 2 @country ASC DIALECT 2
```

### aggregate_load

```text
FT.AGGREGATE qpf_idx * LOAD 1 @amount_raw GROUPBY 1 @country REDUCE SUM 1 @amount_raw AS total SORTBY 2 @country ASC DIALECT 2
```

### search_numeric_count

```text
FT.SEARCH qpf_idx @amount:[100 110] NOCONTENT LIMIT 0 0 DIALECT 2
```

## Validation

Completed trials: 108/108. Total command errors: 0.

Raw records, versions, effective configurations, and correctness hashes: `results.json`.

Reproduce against a new dedicated container:

```sh
docker run -d --name redis-adbc-qpf-bench --cap-add SYS_RESOURCE \
  -p 127.0.0.1:19443:9443 -p 127.0.0.1:12100:12100 redislabs/redis:8.2.0-78.18
uv run --with redis python benchmarks/qpf/run.py
python3 benchmarks/qpf/summarize.py
docker rm -f redis-adbc-qpf-bench
```
