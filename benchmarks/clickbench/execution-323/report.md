# ClickBench execution paths: issue #323

This patch adds checked parallel integer aggregates (Q30), bounded grouping
with exact partial state for regex keys (Q29), compact checked arithmetic group
keys (Q36), narrow partial-group scans (Q32/Q33), and the missing top-K routing
in the streaming fallback (Q26). It also persists execution diagnostics while
a query is running. **The six full-scale timeout reports remain open acceptance
work. These local measurements do not establish completion within 300 seconds
on 99,997,497 rows.**

No paid deployment or deadline extension was performed. Previous official
results and null timeout timings are unchanged.

## Correctness and execution changes

- Q30 uses the existing lazy checked integer input DAG independently in each
  worker. SUM inputs are evaluated per row in aggregate argument order, with
  the existing NULL and overflow behavior; no SUM(x+c) algebraic rewrite is
  used. On an expression failure, remaining workers stop and the pure scan is
  replayed serially to preserve the original reported SQL error.
- Deterministic REGEXP_REPLACE and typed integer `+`, `-`, `*` group inputs
  become eligible for partitioned exact aggregation. Unsupported expressions,
  floating arithmetic, volatile functions, filters and complex aggregates keep
  their established paths. Grouping errors also replay serially.
- Eligible SUM/AVG and string/integer MIN/MAX use exact partial states keyed
  by the final SQL group. Integer sums are arbitrary precision, AVG retains its
  non-NULL denominator, and NULL-only groups remain distinct from zero. Narrow
  scans read exact HASH values when sorting vectors cannot represent them.
  Bounded 8 MiB/65,536-record buffers and 32-way external merges limit retained
  state. High-cardinality partial state can still write more spill bytes than
  source tuples; it does not guarantee compression.
- If an integer grouping expression depends only on columns already included
  as direct group keys, its encoded key is redundant. It is still evaluated
  on every row, in SQL order, to preserve overflow and NULL behavior. Equal
  direct integer keys imply equal deterministic derived values, so removing
  only those encoded components preserves group equality. Expressions whose
  dependencies are not all direct group keys remain encoded.
- Streaming fallback now attempts bounded top-K. The cap-driven HASH scan
  feeds top-K page by page and restores output fields previously removed by
  late projection. HASH pagination still keeps a set of seen keys to suppress
  SCAN duplicates; this set is not bounded by LIMIT.

The [diagnostic contract](../../../docs/execution-diagnostics.md) describes
metric scopes and the distinction between execution return, completion,
validation, and partial evidence. Redis server profiling remains explicitly
unavailable (`null`); client wait measurements cannot identify server CPU cost.

## Local profiles

Redis OSS 8.6.2 ran in the existing local Docker container, with a Go 1.26.6
darwin/arm64 client. The container had no explicit CPU or memory cap. The host
was shared and timing was variable. These are execution-path comparisons
inside the candidate test binary, not isolated before/after driver builds.
The fixture has 20,000 rows and 105 columns, including unrelated wide payloads,
integers beyond 2^53, NULL widths, empty phrases and NUL-containing phrases.

Two distributions use 257 repeated WatchID/ClientIP groups or 20,000 unique
groups. Referer has 31 host groups in both distributions. Sparse sequence IDs
exercise four nonempty worker ranges. Q29 lowers HAVING from 100,000 to 10 for
the local fixture. Table names differ from the official dataset; the final
profiles otherwise preserve the query shapes and ordering, including LIMIT
ties. Q32/Q33 hold concurrency at one to isolate scan/merge changes; Q29/Q30/
Q36 candidates use four workers. Each path/query has three trials.

Median execute seconds (baseline path → candidate path):

| Query | 257 groups | 20,000 groups |
|---|---:|---:|
| Q26 | 0.406 → 0.890 | 0.559 → 0.767 |
| Q29 | 0.290 → 0.325 | 0.552 → 0.366 |
| Q30 | 0.568 → 0.436 | 0.395 → 0.239 |
| Q32 | 1.850 → 0.499 | 1.294 → 0.510 |
| Q33 | 0.812 → 0.428 | 0.803 → 0.496 |
| Q36 | 0.143 → 0.123 | 0.165 → 0.137 |

Q26 did not improve local elapsed time; the top-K change addresses retained
rows and correct routing, while the original timeout's dominant wait remains
unresolved. Q29 timing is inconclusive and its allocation volume increased.
These results do not justify claiming that every query is faster.

Q32/Q33 provide clearer transfer and substage evidence (decimal MB):

| Query/distribution | RESP read MB | Allocated MB | Spill written MB | Merge CPU seconds |
|---|---:|---:|---:|---:|
| Q32 / 257 | 10.75 → 4.64 | 285.1 → 139.4 | 2.293 → 0.034 | 0.097 → 0.026 |
| Q33 / 257 | 8.65 → 4.32 | 255.1 → 139.6 | 1.846 → 0.030 | 0.074 → 0.015 |
| Q32 / 20,000 | 10.83 → 4.68 | 358.0 → 286.8 | 2.362 → 2.407 | 0.150 → 0.119 |
| Q33 / 20,000 | 8.79 → 4.36 | 337.9 → 294.8 | 1.978 → 2.302 | 0.112 → 0.119 |

The high-cardinality case shows the practical limit: wire bytes drop about
57%/50%, but partial states do not reduce spill volume and Q33 merge CPU does
not improve. Allocated bytes are total process allocation volume, not peak
RSS. Production snapshots expose heap size; the trial supervisor also retains
process RSS and CPU. Local measurements cannot establish full-scale memory.

## Independent validation and retained evidence

The full Go suite passed against local Redis, and the new worker, grouping,
spill and live-diagnostic regressions passed with the race detector. The
benchmark checkpoint tests also cover a killed execute process, failed
diagnostic writes, null timeout timings, and unsupported older driver options.

DuckDB independently evaluated the typed fixture and validated all **72 final
trial outputs**. Ordered aggregate validation checks every full projected row,
multiplicity, required higher-ranked groups, result order and the LIMIT rank
boundary against all independently computed groups. Timing completion alone
is insufficient. Validator regressions reject incorrect sums, missing winners,
duplicate groups, lower-ranked rows, wrong order and incomplete output.

- [Low-cardinality trials and snapshots](low-v2-trials.jsonl),
  [independent checks and medians](low-v2-validation.json).
- [High-cardinality trials and snapshots](high-v2-trials.jsonl),
  [independent checks and medians](high-v2-validation.json).
- [Intermediate local experiments](intermediate-trials.jsonl) remain retained,
  including the explicitly invalid initial Q32 baseline fixture that omitted
  a residual input. They are excluded from the final comparison.
- [Environment and source manifest](manifest.json). The fixture hashes are in
  the validation files; the deterministic fixture generator is committed.

Reproduce using fresh output directories:

```sh
cd go
REDIS_URI=redis://localhost:6379/0 \
  REDIS_EXECUTION_PROFILE_DIR=/tmp/issue323-low \
  go test -count=1 -run '^TestClickBenchExecutionProfile$' -v .
REDIS_URI=redis://localhost:6379/0 \
  REDIS_EXECUTION_PROFILE_DIR=/tmp/issue323-high \
  REDIS_EXECUTION_PROFILE_GROUPS=20000 \
  go test -count=1 -run '^TestClickBenchExecutionProfile$' -v .
cd ..
uv run --no-project --with pyarrow --with redis --with adbc-driver-manager --with duckdb \
  python benchmarks/clickbench/validate_local_execution.py /tmp/issue323-low
uv run --no-project --with pyarrow --with redis --with adbc-driver-manager --with duckdb \
  python benchmarks/clickbench/validate_local_execution.py /tmp/issue323-high
```

## Full-scale acceptance still required

Use the unchanged official SQL, three trials per query, the existing
300-second deadline and memory cap, and fresh versioned output paths. Record
driver/binary identity, actual plans/grants and the new internal snapshots.
Validate every completed output with the independent official-data oracle,
including LIMIT ties. Leave timeouts null and preserve all earlier versions.
Q26 requires live plan/wait evidence before attributing its timeout to Redis
sorting, network reads, or HASH fallback. This patch neither provisions a paid
runner nor authorizes extending an existing spending deadline.
