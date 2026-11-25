Target: redis/redis-adbc-driver — comment on #636
---
Two Flex read-path limits recorded during #643/#644 that the directions above don't cover yet.

**1. Reader concurrency is capped at 32.** With Flex, the driver defaults to 32 query workers and a 32-worker budget (`go/database.go:783-807`). `query_workers` and `query_worker_budget` reject values above 32 (`go/database.go:710-711`).

A scratch probe (not committed) on SF1 Flex measured pipelined `HMGET` of six lineitem fields, 256 per pipeline. The deployment was one shard with an 8 GiB RAM tier and verified 4× QPF (`docs/sdlc/changes/tpch-flex-read-path/validation.md:46-53`):

| Connections | 4 | 16 | 32 | 64 | 128 |
|---|---|---|---|---|---|
| Cold, fresh ranges (rows/s) | 15k | 13–18k | 45k | 50–96k | 72–111k |

The record says cold rates "rise with concurrency well beyond the driver's 32-worker maximum". Each cold cell is one 100,000-row range, so the numbers are noisy.

For Q9, moving from 16 to 32 workers turned a 300 s timeout into a validated 266.4 s run. The workers spent 87–90% of their time waiting on replies (`:57-62`). Concurrency above 32 was measured only with the probe (`:74`).

Possible direction: allow a higher Flex reader count, through the option range or the Flex default, after measuring its connection and memory cost.

**2. No Search pushdown after DML overlays.** "After DML, a table's overlay filters are not pushed to Search, so these candidate paths apply to single-segment published tables" (`docs/redis-flex-stopgap.md:57-60`; `docs/sdlc/changes/tpch-flex-q7-q8/spec.md:18`, `validation.md:71`).

So the shared-candidate path that brought SF1 Q8 to 17–38 s (`docs/sdlc/changes/tpch-flex-q7-q8/validation.md:45-53`) is lost once a table receives an UPDATE or DELETE, and reads go back to exact-directory scans. `TestSQLFlexSnowflakeSharedCandidates` checks only the rows after DML, not the plan (`go/join_star_candidates_integration_test.go:121-126`).

This fits this issue's third direction (Search candidate narrowing with exact visibility rechecks). I'm noting it because the overlay case is currently excluded.

Verification ideas: SF1 Flex Q7/Q8/Q9 with more than 32 readers, and a plan assertion after UPDATE/DELETE in that test.
