# Flex unmanaged spec

Enumerate source HASH keys with SCAN and verify each candidate through singleton FT.SEARCH INKEYS counts, including direct key lookups. Respect FILTER/multiple-prefix membership and exclude nested keys for adopted collections. Reject warnings, malformed counts and failed pipelines. Bound membership batches by 128 commands and 1 MiB target (64 MiB single-key ceiling). Deduplicate observed keys; this retains memory proportional to source key count. Require SCAN for enumeration, FT.SEARCH/FT.INFO and native HASH/script permissions. Preserve non-replayable source DML, TTLs, partial multi-row outcomes and DROP/GC ownership. Retain unmanaged ALTER/TRUNCATE/Arrow/shared-snapshot/live-partition exclusions and reject managed foreign imports on Flex.

Related: [intent](intent.md), [spec](spec.md), [plan](plan.md).
