# Plan

1. Separate immutable bucket lookup from leaf emission; add explicit owned probe.
2. Opt in only at the existing direct native CPU parallel source boundary.
3. Bound initial decoded-map capacity by complete projected leaf width.
4. Verify shared/fanout ownership, native multi-page pruning, cancellation,
   errors, compatibility and decoder-inclusive allocation control with pure mocks.
