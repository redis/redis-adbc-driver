# Flex empty-tag compatibility: intent

Date: 2026-11-21. Requestor: repository user. Status: implementation in progress.
Base: `139350b7`; related issue [#566](https://github.com/redis/redis-adbc-driver/issues/566).

The full canonical managed Flex load staged all source rows, then native Search
aborted during TAG index compaction. It never published or reached validation;
see the [measurement](../../../../benchmarks/clickbench/managed-ingestion-2026-11-20/issue-566.md).
The user requested a fix before another full load.

Investigate a driver compatibility workaround that omits empty TAG postings
from newly created plain Flex indexes. Managed Flex reads already enumerate
authoritative directories and evaluate SQL predicates on HASH values. Preserve
SQL empty strings, NULLs, all public columns and publication correctness.

This does not patch the native storage engine, migrate existing physical indexes,
remove all public-column indexes, disable GC, or establish full-scale success.
Native crash contrast and semantic validation are required before claiming that
the workaround resolves the observed failure.

See [specification](spec.md) and [plan](plan.md).
