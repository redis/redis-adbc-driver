# Plan: Cluster API and recovery compatibility

Date: 2026-11-02. Parent: 2734c57. Status: implemented; validation recorded separately.
See [intent](intent.md), [spec](spec.md), and PR #284.

1. Create an isolated replicated Software Cluster API fixture with TLS, AOF and
   verified active 4x QPF, leaving infrastructure state and credentials private.
2. Implement deployment validation, node-level fences, immutable metadata
   directories and proper maintenance client mode. Capture focused regressions before/with fixes.
3. Verify complete multi-shard SQL/import/stream results, stale-writer rejection,
   background jobs, planned failover and restart recovery of committed and private
   state; audit remaining snapshot-only feature gates.
4. Run unit, snapshot, native and fixture checks; review, document concrete
   boundaries and update PR #284. Remove only the owned fixture and its volumes.

See [validation](validation.md) for actual checks and the review. No full legacy
CI or complete platform compatibility claim is made.
