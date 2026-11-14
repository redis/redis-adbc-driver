# Owned string-column ingestion experiment

Requested November 13, 2026. Baseline `06c76c0b`. Reduce per-cell owned string
allocation in Arrow ingestion without borrowing input or combining the rejected
row-matrix reuse candidate. The earlier mixed conversion control still required
roughly 102K allocations per operation. Inspect and measure the narrower owned
string-copy path with matched managed/unmanaged writer controls.

See [contract](spec.md) and [plan](plan.md). No human approval or speedup claimed.
