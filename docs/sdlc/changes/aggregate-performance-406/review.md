# Three-pass agent review

2026-10-21; production b40b88f, test follow-up c064caa, runner 14208bd.
These are the pre-merge agent findings, not human approval. Subsequent status:
Albert merged PR #428 at 2026-10-22T02:03:11Z, and all ten CI checks passed on
ad2faef. The historical full-scale evidence gap below is now resolved by 24 exact
native trials and eight exact profiles; the implementation and issue are closed. See [intent](intent.md),
[spec](spec.md), [plan](plan.md), [validation](validation.json) and the
[benchmark evidence](../../../../benchmarks/exact-aggregate-406/2026-10-21/README.md).

1. **Correctness and lifecycle.** Important finding, reproduced and fixed:
   extending the comparison recognizer to <>/!= let a non-UTC TIMESTAMP versus
   TIMESTAMPTZ predicate enter the ordered localBounds branch. Both operand
   orders returned one row instead of two. b40b88f rejects noninteger not-equal
   before evaluating constants or timezone bounds. Before/after logs are retained.
   Reviewed inclusive rounded bounds, NULL/default widening, exact positive range
   unions, copied list aliases/duplicates, scalar vector ownership, arithmetic
   fallback/error replay and SUM/AVG final overflow. Complete input tuple keys
   retain NULL masks and exact signed integers; only weighted complete tuples
   enter the existing global exact merge. The 4096-entry map flushes before an
   additional unique key; concurrent tail inserts cannot grow it without bound.
   Tests cover flush/partition duplicates, deleted IDs, empty input, overflow,
   cancellation, callback errors, worker admission and both protocols/Cluster.
   Raw scan/HASH replies retain their existing bounded page/pipeline ownership.
   No actionable correctness/lifecycle finding remains in the local checks.

2. **Security and data ownership.** No new SQL identifiers or physical key
   construction enter the driver. The compactor uses existing managed-table
   shape guards and Search discovery; it does not guess dense keys, change
   HASH ownership, write Redis data or claim snapshot isolation. Unsupported
   shapes, residual filters, historical missing-default columns, adopted tables,
   pending cleanup and PushdownNone retain their existing paths. Latency PINGs
   use private timeout clones sharing the owned pool; clones are never closed.
   go-redis resets clone hooks, so these planner PINGs are explicitly outside
   query command diagnostics (server INFO still includes them). Ordinary client
   options and SQL reads are unchanged. Benchmark credentials cross stdin to
   isolated workers and are absent from jobs/results; scanned report artifacts
   contain no credential URIs. Diagnostics contain known public benchmark SQL.
   Full runs journal exact unit names before launch, retain failures, enforce a
   cutoff and require independent confirmation before releasing the window.

3. **Intent, design and evidence (pre-merge; resolved below).** The evidence gap
   at that time was full-scale
   before/after runs and profiles are not yet available. Keep the PR draft and
   #406 open until the recovered dataset passes backup/restore gates and the
   same SQL completes three exact, independently checked trials per case on
   matched resources. Local five-round data shows a strong Q2 gain, lower Q4
   CPU/allocation with nearly flat elapsed time, and lower Q9/Q10 elapsed/server
   CPU at the cost of more commands, network traffic and client allocation.
   The 10ms-added-RTT model selects server tuples for all six Q9/Q10 candidate
   trials, with measured probe overhead. The 2ms threshold is a conservative
   heuristic; it does not estimate bandwidth or guarantee every deployment wins.
   Do not claim reduced Q9/Q10 allocation or universal network speedup. Prefix
   spill/tuple counts match the baseline; full-scale compaction/spill must be
   measured. Separate Go-Arrow profiles include process setup, while their
   logged MemStats delta covers complete query/fetch; native C-Arrow timings
   and exact outputs are recorded separately. At that checkpoint, CI and human
   review were still pending.

Checks inspected/run: full Go (1075 pass events, 17 skips, zero failures), native
ADBC on standalone RESP3/RESP2 and Cluster RESP3 (376 pass, 8 skips, 3 subtests
each), focused standalone/Cluster race tests on both Cluster protocols, exact
predicate and tuple boundary regressions, 64 canonical native comparisons and
eight independently checked complete profiling executions. Raw earlier screens,
failed regressions, provenance corrections, validation and profiles are retained.

2026-10-22 full-window follow-up: reviewed the ownership journal, credential-only
stdin flow, deadline/cgroup bounds, source/binary hashes and independent oracle.
All 24 native trials and eight profiles pass, with no timeout/OOM/mismatch; all
34 services are inactive and the report/archive checksums verify. The owner
independently accepted release and resumed its API scans. Full profiles confirm
Q4 map-decode CPU reduction and Q9/Q10's increased client CPU/allocation/RESP
costs alongside lower elapsed time and server CPU. Sampling and overlapping
worker/cumulative-stack limitations remain explicit. No actionable finding in
this evidence follow-up; production behavior is unchanged by this branch.
