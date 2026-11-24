# Plan and evidence

Implemented two narrow eligibility/callback changes in scalar_aggregate_scan.go and scan_projection.go. Added parity tests for NULL, duplicate values, defaults, explicit NULL markers, malformed fields before rejection, unsupported expressions, parameters, and exact integers above 2^53.

Independent review found a positional hashExclusive callback could bypass a residual. Fixed it by refusing positional binding when a bound predicate exists and defensively rejecting hashFetch plus a bound predicate. A reviewer-owned no-socket transport test exercises the actual scalar scan and authoritative native HMGET callback wiring: three candidates (including NULL), only exact matching row ID2 consumed, processed3/matched1.

Focused unit selection passed (0.731 s), Redis environment variables unset, GOMAXPROCS=2, GOMEMLIMIT=768MiB, -count=1 -p1. Includes TestManagedAggregateResidual*, TestScanProjection*, TestUnmanagedFilteredProjection*, TestSnapshotNarrowProjection*, and native HASH compatibility regression. No live Q9 success or speedup is claimed yet. Human maintainer review pending.

Next: build an immutable native candidate and rerun only Q9 on preserved Software Flex SF1 with verified 4× QPF; fetch and independently validate every successful timed result.

See [intent](intent.md) and [spec](spec.md).
