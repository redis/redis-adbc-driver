# Validated point-read protocol: intent

Date: 2026-11-14. Source: `b904cf8d` (merged #439). Status: candidate native missing-row proof improvement and diagnostic tooling; larger server contract remains proposed, with no approval inferred. Requestor: Albert Wong; author: Codex.

A complete SQL point read is approximately 2.9 ms, versus 0.236 ms for redis-py in the [selected native report](../../../../benchmarks/oltp-query-efficiency/2026-11-14-final/report.md). Managed and unmanaged reads both pay catalog resolution costs. That comparison measures different guarantees and complete public paths, rather than isolated Redis command execution.

The user requested performance work using subagents. This work identifies the next dependency that cannot be removed by speculative caching and adds a reproducible attribution workload. It adds a native two-command missing-row proof pipeline and fixes uncertain absence classification. It also supplies a concrete contract for an upstream combined descriptor/lookup primitive; it does not claim that the server implements that API.

Success means identifying precisely which native proofs, permissions, key discovery and exact row semantics must survive a combined operation, with runnable hit/miss/overlay attribution and explicit implementation gates. The rejected [root-prefetch experiment](../point-root-prefetch/validation.md) remains rejected. No new cache, lease relaxation, generic scripting ACL grant, unmanaged consistency promise, automatic retry, storage-format change or ClickBench run is in scope.

See [specification](spec.md), [plan and verification](plan.md), and [protocol proposal](../../../validated-point-protocol.md).

Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
