# Specification

Date: November 14, 2026. Author: Codex. Status: focused validation complete.
[Intent](intent.md), [plan](plan.md), [validation](validation.md).

1. Queue the unchanged protocol-17 lease-valid command after the existing
   manifest/directory/map proof commands in one native pipeline. Keep all keys,
   namespace arguments, proof checks and native error precedence. Shared binding
   terminal-loss handling stays on its existing path. Every proof still reaches
   Redis; no time-based lease cache or removed buffered-batch validation.
2. A definite top-level NOSCRIPT/EVALSHA-command refusal can execute its identical
   full-body lease command once. Do not replay metadata reads, a valid operation
   with an unknown outcome, or a pipeline whose populated replies accompany a
   transport/hook failure. Preserve materialized-reader expiry diagnostics.
3. A committed descriptor GET shares its existing immutable-value presence/ref
   proof pipeline. Return that exact native GET reply. Perform GET/key ACL
   checks again on each load; cache neither its value nor authorization.
   Mutable workspaces use their original path. Record metadata dependencies as
   before. Legitimate absent metadata stays absent; missing referenced values,
   invalid references, native GET errors and unknown pipeline errors fail closed.
4. Known-key routing, newest segment/tombstone selection, external index/row
   membership, final read validation, cancellation and stream lifetimes remain
   unchanged. Cluster pipelines retain explicit same-slot keys and no retry.

Acceptance: deterministic tests verify wire argument equality, one proof pipeline,
proof/error ordering, definite-only digest fallback, denied/absent/WRONGTYPE GET,
no data/proof cache across pins and no unknown-reply replay. Verified Software
checks cover corruption, old pins across drop, mutable/private descriptors,
restricted/shared streams, expiry/cancellation and unmanaged freshness. Native
before/after measurements remain the performance evidence, rather than small
instrumented wall-time samples.

Implementation and evidence: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).
