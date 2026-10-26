# Direct-slice contract

2026-10-21. Baseline `a937c55b`. See [intent](intent.md), [plan](plan.md).

Only while the existing Arrow projection callback is active on a guarded wide
HASH-fetch scan, read HMGET's positional values directly into prebound column
slots. Keep identical Redis commands, protocol (RESP3 default), discovery pages,
pipeline bounds, row existence/readiness and Arrow batch settings. Validate the
internal ID first and all needed columns in metadata order before appending any
Arrow values. Preserve renamed HASH fields, alias/duplicate outputs, temporal
and exact numeric types, missing defaults versus explicit null markers.

Generic execution retains maps. Missing-default fallback may build a temporary
raw row to reuse existing missing-value semantics. Stream callbacks cannot
escape a fill. Results must own Arrow data after input/scratch/release changes.
Prove both APIs with full-value/multiplicity validation, repeated matched trials,
narrow controls, standalone/Cluster and RESP2 compatibility checks. Publish
negative command-allocation screens as well as winning variants.

Implementation: [PR #422](https://github.com/alberttwong/redis-adbc-driver/pull/422).
