# Plan

Date: 2026-11-23. Status: local implementation and verification.
See [intent](intent.md), [specification](spec.md), [verification](verification.md).

1. Add worker-owned probe scratch and original-fact bucket lookup.
2. Replace per-dimension map merges with one ordered complete leaf copy.
3. Compare exact bags/order with a frozen copy of the preceding algorithm.
4. Test NULL/missing/unmatched keys, right overwrites, provenance, independent
   outputs, scratch reuse, consumer errors, cancellation and concurrent workers.
5. Retain an allocation regression and controlled microbenchmark for Q4-width
   projection, duplicate Cartesian matches and a late dimension nonmatch.
6. Run focused race and full unit checks. Native query timing remains separate;
   full benchmark suites stay stopped and earlier revisions remain available.
