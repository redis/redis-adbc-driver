# Plan: snapshot compatibility

Date: 2026-11-02. Source: 7c1a7ca. Status: implemented and locally validated; draft PR pending human review.
See [intent](intent.md), [spec](spec.md), and PR #284.

1. Implement private ALTER rewrites using existing SQL conversion/constraint logic.
2. Include temporary objects in internal manifest traversal and snapshot cleanup.
3. Restore HASH tools and external descriptors with explicit ownership guarantees.
4. Restore partition production with correct visibility and source lifetimes.
5. Run focused regressions, unit checks, snapshot race and native checks against
   disposable Redis Software with verified active 4x QPF; review and publish PR.

See [validation](validation.md) for completed checks, review and limits.

Implementation preserves RESP3, execution transports, Arrow ownership and background
compaction fences. Protocol 5 rejects old writers that would omit references.
External sources use managed copies per the user decision, not live registrations.
