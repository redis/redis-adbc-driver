# Contract

Date: 2026-11-06. Base: `9c838db7`. Issue: [#341](https://github.com/redis/redis-adbc-driver/issues/341). Status: in progress.

Inventory public HASH inspection/copy/adoption/refresh/sweep and SQL maintenance entry points, including input retention and failure boundaries. Do not describe unreachable legacy branches as supported utility limits. Keep bounded control admission and the 64 MiB single-row packet ceiling; do not raise constants.

Within a private managed output, acknowledged TAG/NaN capability facts are monotone. Streamed batches must not rewrite unchanged facts merely because their input descriptor predates earlier batches. Published/shared descriptors retain their existing update protocol. Unknown metadata outcomes abort without replay; no acknowledgement cache may hide an error.

A retained `HashCopy` result must preserve its statistics without keeping the
input reader, seen-key map, Arrow record or closed connection objects alive.
The copy may still retain these inputs while executing; returning statistics is
not permission to extend their lifetime. Preserve the result's existing fields
and map counts without changing its public API.

Verify bounded metadata work, exact row/byte boundaries, full successful copies, partial-error/cancellation cleanup, old-reader/reference safety and unchanged external sources on Redis Software with verified effective 4x QPF. Report complete-operation allocations/retention honestly, including operation-sized input/key retention where it remains. No public process-resume claim or storage upgrade.

See [intent](intent.md), [plan](plan.md), [validation](validation.md).
