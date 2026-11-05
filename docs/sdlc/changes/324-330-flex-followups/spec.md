# Flex follow-up contracts

Date: 2026-11-05. Status: implementation in progress. See [intent](intent.md) and [plan](plan.md).

Retained execution carries the producer's Flex mode into its independently budgeted source client. Existing SCAN/HMGET capture, admission, source identity/cardinality checks, immutable Arrow publication, namespace ACLs, expiry, cancellation and cleanup contracts remain. Capture freezes observed values for one execution; no snapshot isolation is added. Reader-only replay reads published retained data and does not require Search or catalog mutation, including on Flex.

Schema/index/rekey changes require actual Flex coverage of plain schema generation, readiness, rollback/recovery and row ownership before guards can be removed. Default RAM paths retain their existing behavior.

Deployment and performance claims must name actual topology, capabilities and measurement scope. A provider limitation or incomplete operator test cannot be reported as successful compatibility.
