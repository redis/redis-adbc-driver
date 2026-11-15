# Exact unmanaged source projection: intent

Date: 2026-11-14. Requestor: repository user asking for performance work with
subagents. Driver baseline: merged PR #440, `d7c0978f`. Upstream Search source:
`33d90342bc9c601d904fb58446f8bda4eaba5011` (master verified through GitHub).
Status: uninstalled upstream library prototype; seven core tests, real native API type-check and Linux baseline RESP3/typed-ADBC repro passed.

Filtered unmanaged aggregates still discover candidates through Search and read
about 10,000 live HASHes per query in the existing benchmark. Argument boxing
reduced allocation but did not improve SQL latency; it remains rejected.
The desired structural improvement is projected authoritative source reads
inside Search's existing candidate traversal. This work implements a reviewable
primitive foundation and adversarial repro rather than an unsupported driver
capability or approximate reducer.

The existing [Search #12334](https://github.com/RediSearch/RediSearch/issues/12334)
already covers the requested source/reduction boundary; no duplicate issue or
external comment is created here. See [contract](spec.md), [plan](plan.md), and
[prototype/repro](../../../../benchmarks/unmanaged-source-projection/README.md).

Review: [PR #441](https://github.com/redis/redis-adbc-driver/pull/441).
