# Plan

Date: 2026-11-03. Baseline: `36aa28d`. Author: Codex.
See [intent](intent.md), [spec](spec.md), and [validation](validation.md).

1. Add an isolated internal scalar/native predicate package; leave SQL paths alone.
2. Check all comparisons against an independent boolean oracle, extremes, NULLs,
   odd tails, batch boundaries, invalid shapes, cancellation and buffer ownership.
3. Add kernel and full conversion benchmarks and native amd64/arm64 CI runs.
4. Profile a disposable authenticated TLS/RESP3 Software fixture after setup
   verifies active 4x QPF. Fully fetch and validate SQL outputs and int64 types.
5. Record actual environments, costs, skips and go/no-go decision. Keep operational
   traces and identifiers private. Publish only synthetic measurements and summary.
6. Submit for review; inspect checks and repository merge requirements before
   merging and closing the linked issue. Do not equate unrun hardware with parity.

Implementation, local Software profiling, native amd64/arm64 measurements and
self-review are complete. The linked PR carries final repository checks and
merge/closure status. Outcome: retain the isolated prototype; do not wire it
into SQL execution.
