# Contract

Date: 2026-11-20. Initial source baseline: `845c95b4`; rebased onto `1d0d0b3b`. Requestor: Albert Wong. Author: Codex.
Status: implemented and locally validated; awaiting human review. No release recorded.

Eligible ungrouped COUNT/SUM/AVG/MIN/MAX scans may divide exact signed-int64 row-ID intervals across bounded workers. Each worker seeks every immutable segment directory to its interval, resolves newest versions/tombstones before evaluating the complete predicate, and reads only winning HASH values. Flex needs no aggregate cursor, SORTABLE, or rounded numeric Search boundary.

Reuse exact accumulators and ordered partial-result combination, existing per-query ceilings/shared per-handle budget, telemetry/autotuning, leases and cancellation ownership. Keep unsupported expressions/grouped/DISTINCT/FILTER shapes on established fallback paths. A worker failure must cancel/join peers before returning its grant, and no successful partial aggregate may escape.

All workers share the statement snapshot through completion. Publications, compaction and GC cannot change its inputs. Source ownership remains driver-managed; no application-owned modes or ADBC partition descriptor protocol changes are included. Standard Search-safe paths remain available; exact directory ranges admit native IDs beyond 2^53 where supported. Flex retains its existing allocation ceiling.

Validate exact rows/types against one-worker execution, independent oracles, overlays, tombstones, sparse/high IDs, predicate fallback, pin expiry, cancellation, permission/transport failures, and worker-budget cleanup. Use Redis Software RAM and actual compatible Flex with verified active4x QPF. Measure fixed/adaptive workers only after complete result validation; disclose scale, CPU/memory, route and limits.

See [intent](intent.md), [plan](plan.md), [validation](validation.md).
