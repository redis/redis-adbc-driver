# Complete native CRUD/query ABBA comparison

`compare.py` is an offline, stdlib-only auditor/comparator for four **complete**
[ordinary scenario](../table-modes/scenarios.py) windows: baseline A1, candidate
B1, candidate B2, baseline A2. Each window must have all 168 records/42 cases:
managed/unmanaged/redis-py, one/four clients, four point operations and three
analytical queries, with one warmup plus three measured rounds. There are 672
records in total, including 168 warmups and 504 measured trials.

The existing strict `chart_data.scenario_summary` validates each complete matrix
and all retained per-trial arithmetic before comparison. Additional checks require
the exact current four-column generator, 1K point/100K analytical fixtures,
32 point calls/eight queries, trusted TLS/RESP3 declaration, effective 4× QPF and
six Search workers/mnp32/4-GiB policy, identical scenario-source hashes/packages
and full metadata. Baseline source/library/version must be identical in A1/A2;
candidate identity must be identical in B1/B2. Between builds, only the native
source/library/version fields may differ. Expected source commits can be supplied
explicitly; the CLI defaults baseline to clean main `3b422454`.

```sh
python3 benchmarks/crud-execution/compare.py \
  --a1 /private/A1/results.json --b1 /private/B1/results.json \
  --b2 /private/B2/results.json --a2 /private/A2/results.json \
  --candidate-source FULL_CANDIDATE_COMMIT \
  --out /private/new-comparison
python3 -m unittest discover -s benchmarks/crud-execution -p 'test_*.py'
```

The tool records every input hash without publishing operational paths. A fresh
output directory is mandatory. `comparison.json` retains all four 42-case
summaries, all 42 pooled cases, both complete 42-case forward/reverse comparisons,
and every raw Redis control. `comparison.csv`, `pairs.csv` and `report.md` expose
the same complete cases, including slower results. Reverse-pair numeric arrows
always mean baseline→candidate, despite candidate executing first.

P50 and operation means pool measured operation observations; p95 uses nearest
rank. Each pooled point/build cell has 192 observations and each analytical cell
48. Pair cells have 96/24 observations. All calculations use unrounded retained
samples; only Markdown formatting rounds displayed values. Each build retains
its own raw Redis median and elapsed multiple; control drift is never normalized
away or treated as a causal driver speedup.

Two finite throughput metrics are explicitly named: total completed operations
divided by summed trial wall times, and median per-trial operation rate. Trial
wall time includes coordination/scheduling. Neither is saturated capacity, and
the inverse of a pooled operation mean is not concurrent workload throughput.

The declared target is **at least 10% less pooled one-client point-read p50 in
both managed and unmanaged modes**, equivalently candidate≤0.9×baseline for each
mode independently. The target result is always reported. A valid comparison
can be exported even when the target is not met; add `--require-target` to return
nonzero after retaining the complete output if that gate fails. Other cases,
p95/mean regressions and raw controls remain visible regardless of the target.
A passing target does not establish that every tradeoff is acceptable.

The native C/Python driver/CLI builds, image/runtime/resource settings, actual
window sequencing and frozen helper identities still need independent owner
provenance. JSON metadata alone does not authenticate executed binaries or prove
chronological order. These finite ABBA windows do not establish confidence
intervals, production capacity, server-CPU attribution or identical consistency
semantics between managed SQL, live source writes and Redis commands. Lazy
first-operation transport work remains included; `automatic_retries=0` declares
no caller replay, not wire retry/reconnect telemetry. No live Redis call,
ClickBench run, cloud provisioning or historical relabeling is performed by this
auditor.
