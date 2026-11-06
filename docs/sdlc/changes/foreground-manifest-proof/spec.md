# Manifest proof contract

See [intent](intent.md) and [plan](plan.md). Parent production: `2eeb82d9`.

Read the pinned committed work record, native-key directory cardinality and
root-map cardinality in one ordinary read-only pipeline. The pinned committed
root is immutable and cannot be reclaimed while the reader lease is valid.
These reads were previously serial, not an atomic Redis transaction; batching
does not introduce a historical-view or stronger atomicity claim.

Retain the original proof requirements and error precedence: readable/valid
committed manifest with a nonnegative key count, matching directory cardinality,
valid 0..256 bucket count, and matching map cardinality. Inspect individual
command errors before consuming their values. Any failed/incomplete proof aborts
acquisition or workspace creation; never fall back to live data or replay input.
Existing lease validation, native ACL checks and write-outcome handling remain.

Attribute fixed UPDATE/no-op and Arrow-small/batch work with 0/32 unrelated tables.
Controlled profiles disable maintenance and keep each target below the segment
admission limit; they are not sustained throughput benchmarks. Matched public
native comparisons must retain all samples, finish publication/fetch and verify
all rows independently on effective-QPF4 Redis Software. No competing local
build/test/profile process during comparison windows. Report regressions and
explicit skips; do not infer a gain from the round-trip count alone.
