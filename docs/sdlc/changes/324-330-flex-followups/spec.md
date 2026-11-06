# Flex follow-up contracts

Date: 2026-11-05. Status: implementation/evidence merged in [PR #331](https://github.com/redis/redis-adbc-driver/pull/331). See [intent](intent.md) and [plan](plan.md).

Retained execution carries the producer's Flex mode into its independently budgeted source client. Existing SCAN/HMGET capture, admission, source identity/cardinality checks, immutable Arrow publication, namespace ACLs, expiry, cancellation and cleanup contracts remain. Capture freezes observed values for one execution; no snapshot isolation is added. Reader-only replay reads published retained data and does not require Search or catalog mutation, including on Flex.

Schema/index/rekey changes require actual Flex coverage of plain schema generation, readiness, rollback/recovery and row ownership before guards can be removed. Default RAM paths retain their existing behavior.

Deployment and performance claims must name actual topology, capabilities and measurement scope. A provider limitation or incomplete operator test cannot be reported as successful compatibility.

The Flex page ceiling is 10,000 IDs, reduced by width and actual server caps. This changes request batching, not completeness checks or the consistency contract. The profile harness records query timestamp intervals for 20 ms server RSS sampling and independently checks the full result stream. Tiered/concurrent profiles use 150,000 4 KiB payloads and compare default Arrow row batching with the existing 8 MiB byte budget. No new default Arrow byte limit or general external sort spill is introduced.
