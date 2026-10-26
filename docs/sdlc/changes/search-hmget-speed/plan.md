# Search discovery and HASH-fetch verification

Review: [PR #419](https://github.com/alberttwong/redis-adbc-driver/pull/419).

2026-10-21. Requested by Albert Wong. Base `4b0fdb5`, production `dbdafa2f`,
final focused tests/link helper `51baf9d0`. See [intent](intent.md), [spec](spec.md),
[results](../../../../benchmarks/search-hmget/2026-10-21/README.md) and
[validation](validation.json). Local work complete; CI and human review pending.

1. Completed isolated, reproducible three-round map-merge/map-adoption/batch-size
   screens. Fixed the initial guard that excluded the regular implicit row-ID
   sort; retained initial controls/interrupted artifacts.
2. Implemented the guarded 64-column/more-than-half LOAD-all rule. Kept generic,
   narrow and unsupported ownership/operator shapes on their existing paths.
3. Completed five-round wide confirmation with historical native settings, then
   another five-round comparison with 4,096-row native pipeline/page sizes. Both
   APIs improve repeatedly; native partition reads remain faster.
4. Completed 64-column boundary checks, initial and repeated narrow controls,
   calibrated latency/bandwidth checks, bidirectional bytes and three-scan Go
   profiles. Corrected emulator connection-task cleanup and repeated the entire
   model matrix; final shutdown is clean. Retained earlier observations.
5. Completed standalone/Cluster correctness, upstream native validation and
   focused race checks. Strengthened independent wide assertions for implicit
   order, empty results and LIMIT/OFFSET, then reran the final full Go suite.
6. Reviewed all three REVIEW.md passes:
   - Correctness/lifecycle: full values and types, aliases/duplicates, null
     markers/defaults, all-NULL existence, sparse/deleted IDs, concurrent HASH
     update/delete windows, index ordering, readiness flow, cancellation,
     cursor/worker ownership and retained Arrow records. No remaining actionable
     finding in the scoped change.
   - Security/ownership: existing managed-table gate excludes application-owned,
     foreign, key-row, temporary/rekeying layouts. Keys come from Search; no
     guessed-key production path, new ACL commands, TLS changes or writes.
     Diagnostic endpoints bind localhost. No remaining actionable finding.
   - Intent/design/evidence: pinned binaries/source/fixtures, matched RESP3 and
     Arrow output, interleaved complete fetches, raw initial failures/controls,
     native tuning, memory/bidirectional-traffic tradeoffs and model limits are
     explicit. No clean server/socket-wait wall-time decomposition is claimed;
     no Cloud/Software/other-architecture performance is inferred.
7. Completed cleanup of the three task-owned Docker servers and the proxy; no
   unrelated resources were changed. Publish a reviewable PR; no merge,
   deployment or human approval is inferred.

The original issue remains linked rather than auto-closed: broader deployment
performance and any later command-argument/map optimization can be tracked there.
