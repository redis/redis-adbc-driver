# Allocation and readiness evidence

Source: `09588f0ca874a34cfbf092cf39ecdf06cbed6a0c`. Gate: **cancelled**. 0/0 cases pass; 54 incomplete or invalid cases; 141 recorded results.

Holding is a diagnostic phase, not proof of convergence or a global optimum. Fractions weight snapshots by the number of completed queries. Target changes cover learning and measurement; holding fractions cover measurement. Reason counts are snapshots, not counts of independent decisions. Continuous snapshots can overlap other in-flight queries.

| Case: deployment / rows / CPU / link / arrivals / concurrency | Holding fraction median (range) | Target changes median (range) | Final retained workers by repetition |
|---|---:|---:|---|
| oss / 32768 / 1 / direct / waves / 1 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| oss / 32768 / 1 / direct / waves / 2 | 1.00 (1.00–1.00) | 3 (3–3) | [2] |
| oss / 32768 / 1 / direct / waves / 4 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| oss / 32768 / 2 / direct / waves / 1 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| oss / 32768 / 2 / direct / waves / 2 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| oss / 32768 / 2 / direct / waves / 4 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| oss / 32768 / 4 / aggregate-response-2ms / waves / 1 | 1.00 (1.00–1.00) | 2 (2–2) | [4] |
| oss / 32768 / 4 / aggregate-response-2ms / waves / 2 | 1.00 (1.00–1.00) | 2 (2–2) | [4] |
| oss / 32768 / 4 / aggregate-response-2ms / waves / 4 | 1.00 (1.00–1.00) | 2 (2–2) | [4] |
| oss / 32768 / 4 / direct / waves / 1 | 1.00 (1.00–1.00) | 6 (6–6) | [6] |
| oss / 32768 / 4 / direct / waves / 2 | 1.00 (1.00–1.00) | 2 (2–2) | [4] |
| oss / 32768 / 4 / direct / waves / 4 | 1.00 (1.00–1.00) | 2 (2–2) | [4] |
| oss / 100000 / 2 / direct / continuous / 1 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| oss / 100000 / 2 / direct / continuous / 2 | 0.00 (0.00–0.00) | 5 (5–5) | [2] |
| oss / 100000 / 2 / direct / continuous / 4 | 1.00 (1.00–1.00) | 2 (2–2) | [2] |
| oss / 100000 / 2 / direct / waves / 1 | 1.00 (1.00–1.00) | 5 (5–5) | [4] |
| oss / 100000 / 2 / direct / waves / 2 | 1.00 (1.00–1.00) | 2 (2–2) | [2] |
| oss / 100000 / 2 / direct / waves / 4 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| software / 32768 / 1 / aggregate-response-2ms / waves / 1 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| software / 32768 / 1 / aggregate-response-2ms / waves / 2 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| software / 32768 / 1 / aggregate-response-2ms / waves / 4 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| software / 32768 / 1 / direct / waves / 1 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| software / 32768 / 1 / direct / waves / 2 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| software / 32768 / 1 / direct / waves / 4 | 0.33 (0.33–0.33) | 5 (5–5) | [2] |
| software / 32768 / 2 / direct / server-transition / 1 | 1.00 (1.00–1.00) | 2 (2–2) | [2] |
| software / 32768 / 2 / direct / server-transition / 2 | 0.33 (0.33–0.33) | 6 (6–6) | [3] |
| software / 32768 / 2 / direct / server-transition / 4 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| software / 32768 / 2 / direct / waves / 1 | 1.00 (1.00–1.00) | 2 (2–2) | [2] |
| software / 32768 / 2 / direct / waves / 2 | 1.00 (1.00–1.00) | 2 (2–2) | [2] |
| software / 32768 / 2 / direct / waves / 4 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| software / 100000 / 2 / direct / waves / 1 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| software / 100000 / 2 / direct / waves / 2 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |
| software / 100000 / 2 / direct / waves / 4 | 1.00 (1.00–1.00) | 4 (4–4) | [3] |

## Old-policy diagnostic comparisons

Both policies use the same profiler, fixtures, quotas and randomized experiment. These comparisons cannot retroactively control the original workstation run. Ratios are new policy / old policy medians; above 1 favors throughput, below 1 favors p95.

| Case | QPS ratio | p95 ratio |
|---|---:|---:|

## Interpretation limits

The warmed gate uses the same highest-throughput fixed policy for its throughput and p95 reference. Server-transition windows include adaptation, rather than a stationary server. Cold learning excludes fixture loading/reference execution. A fresh handle is used per trial and shared across concurrency phases.

The plan requires three repetitions. Tables show only collected repetitions; missing repetitions cannot establish uncertainty or readiness. Completed-case uncertainty uses full min/max spread, not a confidence interval. Host/VM activity and proxy overhead remain possible contributors. An equal retained worker count alone cannot establish that any observed regression was caused by the tuner; check grants, phase histories, CPU/throttling, and run spread.

See `REPORT.md` and `gate.json` for every bound and resource summary, `results.jsonl.gz` for complete per-query and allocation evidence, `manifest.json` for the predeclared experiment, and `completion.json` for infrastructure results.
