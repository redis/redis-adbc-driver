# Specification

The native CPU parallel source explicitly transfers ownership of each newly
allocated decoded fact map. Every dimension key is resolved against the original
fact before any mutation. A missing/NULL key emits nothing. If any bucket has
multiple rows, the existing independent fresh-leaf Cartesian expansion remains.
With all singleton buckets, extend the fact in dimension order and emit it once;
right-side collisions, including physical row ID, keep the prior overwrite rule.
Dimension maps, raw HASH maps, memory/CTE inputs and sibling outputs stay immutable.

Only this direct native CPU request uses the owned probe. Shared/serial, GPU and
UNION paths use the established fresh-leaf probe. Existing retained/private and
unsupported admission fences remain. CPU keep() creates a fresh typed map for
each fetched row after identity/visibility checks; the full residual runs before
consumer delivery. Source progress uses separate exact directory IDs, never the
typed map after delivery. Tests allow consumers to clear emitted maps across
native pages to enforce this boundary.

The direct source reserves at most 64 map entries, calculated from complete
projected need plus physical row ID. Ordinary decodeRow allocation is unchanged;
negative/excessive hints are ignored. Nested source literals do not inherit hints.
This affects capacity only, never projected values or types. Cancellation,
backpressure, source error precedence, leases, grants and panic joins are unchanged.
