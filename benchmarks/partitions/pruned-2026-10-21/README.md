# Selective partition comparison, 2026-10-21

Local supplemental SQL on the first 100,000 official ClickBench source rows,
105 columns, retaining every tenth allocated ID (10,000 live rows). This is
not official leaderboard SQL or a full-scale/Cloud result. The source prefix,
upstream revision, library hashes, package/server versions, all repetitions,
full schemas/values and raw telemetry are in the JSON files.

Before: preserved #385 native library, SHA256
`f42eba950e767cc3c2bed3346e10f6ed2091493955a3bd4e91be4c747197ec75`.
After: #380 implementation including the fractional ordinary-key correction,
SHA256 `ad724cddd8ab448a512ef011b62cf63ea506d8ce156100d8040b4f30d60d2fbd`.
Both used Redis 8.6.2/Search 8.6.0 and actual aggregate cap 2147483648 on
localhost:17479, eight requested descriptors, rowid strategy, one/four workers,
three repetitions. The host was not exclusive; tests ran after these trials.
The before JSON's driver_revision names the harness checkout, not the preserved
binary's source; library hashes establish binary identity.

## Nonempty sparse fixture

All 54 trials completed with exact source/DuckDB value and ordinary Arrow schema
checks. End-to-end includes planning, fresh worker processes/connections,
complete fetch and Arrow IPC, excluding parent validation. Medians, four workers:

| Predicate | Rows | Descriptors before → after | Full fetch before → after | Planning before → after |
|---|---:|---:|---:|---:|
| `__rowid=100000` | 1 | 8 → 1 | 0.6585s → 0.2485s | 0.03083s → 0.02821s |
| IN(100000,99990,100000,99980) | 3 | 8 → 3 | 0.5252s → 0.2676s | 0.02436s → 0.02943s |
| BETWEEN 98001 AND 100000 | 200 | 8 → 8 | 0.4658s → 0.3648s | 0.02630s → 0.02646s |

First four-worker repetition's Redis command calls (including planning/startup):

| Predicate | HMGET before → after | FT.AGGREGATE before → after | FT.INFO before → after | CONFIG GET before → after |
|---|---:|---:|---:|---:|
| Point | 10000 → 1 | 16 → 1 | 48 → 0 | 16 → 0 |
| IN | 3 → 3 | 16 → 3 | 48 → 0 | 24 → 0 |
| Range | 200 → 2000 | 16 → 8 | 48 → 0 | 24 → 0 |

The remaining FT.AGGREGATE calls are connection startup capability probes;
the direct reader does not consume Search rows and works with that command
denied. Pipeline pages are bounded, but scanning a proven sparse interval can
issue more HASH commands than Search. The retained candidate envelope limits
that work; no universal speedup or reduction in command count is claimed.
Ordinary ExecuteQuery still avoids process/descriptor overhead for these small
results; its measured times are retained in the raw summaries.

Maximum sampled process-tree RSS (MB decimal): point 1180.8 → 1043.4,
IN 776.9 → 1260.3, range 776.0 → 1370.6. Maximum sampled Redis used memory was
about 149MB for both libraries. RSS includes the source/oracle/coordinator,
imports, workers and shared pages, sampled every 50ms with no enforced limit;
these observations do not establish a memory improvement or whole-dataset
memory bound. Reader buffering bounds are structural, not inferred from RSS.

## Small dense fixture

Another 24 exact-value/schema trials used 1,000 allocated/live rows, three
repeats, eight requested descriptors and four workers. Point medians improved
0.6141s → 0.2576s (8 → 1 descriptors); IN improved 0.5184s → 0.2871s
(8 → 3). First point repetition HMGET calls were 1000 → 1, FT.AGGREGATE
16 → 1, FT.INFO 48 → 0, CONFIG GET 16 → 0. IN HASH calls stayed at 3;
Search/readiness/CONFIG overhead was removed as on the sparse fixture.
Raw planning times, repeated command counts and memory samples are retained in
before-small.json/after-small.json. Maximum sampled client RSS: point
733.2MB → 404.5MB; IN 732.3MB → 620.1MB, with the same sampling limitations.
The after library was rebuilt after merging main, with identical Go runtime
changes; its build/VCS metadata hash is
`384dafdba6dd4c78f669111563c783a8bc7ed2c6e4902fd5dabf7c1f7a3bf7d0`.
Use --rows 1000 --layout dense --workers 4 and the point/IN workloads to repeat.

## Reproduction

Use `benchmarks/clickbench/partition_profile.py` with a preserved/new native
library, the official upstream checkout and the same 100k source prefix:

```
--rows 100000 --layout sparse --repeats 3 --partitions 8 --workers 1 4
--workloads rowid_point rowid_in rowid_range --output <fresh JSON>
```

Set REDIS_URI to the dedicated server and pass --driver, --upstream and --parquet.
The harness validates every complete result and rotates API order per repeat.
Before/after libraries ran sequentially, not interleaved; absolute timing and
sampled-memory differences remain subject to host variation. Supplemental SQL
is explicitly labeled; all 43 official queries retain their coverage records.
