# Intent

2026-11-23. Status: implemented, focused unit review complete; live validation pending.

TPC-DS Q9 repeatedly scans managed rows for scalar COUNT/AVG expressions with integer residual filters. The scalar input vector path currently refuses these predicates and allocates a decoded map per row. Reuse the existing exact bound predicate evaluator after authoritative fetching and metadata-order decoding. Ordinary projections and grouped aggregate eligibility stay unchanged.

See [spec](spec.md) and [plan](plan.md). Full suites and TPC-H remain stopped.
