# Plan

See [intent](intent.md), [contract](spec.md).

1. Profile fenced bulk writes and complete wide/narrow/filtered/overlay reads.
2. Capture a focused case for avoidable Search control traffic in a committed
   single-generation row-local scan; benchmark directory merging and row sizing.
3. Reuse the immutable reader and remove redundant allocation/formatting work.
4. Run exact-value, snapshot isolation, ACL, cancellation, partition, projection,
   write-outcome and byte-bound tests plus unit/build checks.
5. Record actual before/after observations, remaining bottlenecks and self-review.

Completed implementation, focused failing-before regression, unit/build checks,
Software race checks, exact before/after profiling and self-review.
See [validation](validation.md) for skips, measured scope and remaining write costs.
