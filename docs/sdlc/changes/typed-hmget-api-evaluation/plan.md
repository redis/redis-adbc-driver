# Plan and disposition

1. Implement/review the upstream API and validate actual command, cache, owned
   results, hooks, pipelines and Software paths; retain failed attempts.
2. Freeze exact pinned/master API commits plus driver overlay/module/source
   hashes before building or measuring; preserve reproducible unapplied patches.
3. Run current-master parser twelve cases × three observations with complete
   byte/NULL validation, followed by separately pinned complete native28+28.
4. Audit all36+56, timing/RSS arithmetic and compound provenance; disclose fixed
   ordering, scope, failed hook test and pre-existing master mock build failure.
5. Publish evidence and an opt-in private-worktree runner. Keep default driver
   code/module unchanged: the native candidate regressed every measured case.
   Parent completed final shared fixture/credential teardown after all tracks.

See [intent](intent.md), [contract](spec.md) and [validation](validation.md).
