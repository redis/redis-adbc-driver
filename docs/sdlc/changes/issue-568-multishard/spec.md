# Multi-shard managed ingestion: specification

Date: 2026-11-20. Source: `1b5918e6`. Status: proposed design boundaries, not approval.
[Intent](intent.md) · [Design](design.md) · [Plan](plan.md)

## Required properties

1. One logical schema/table exposes a single pinned committed catalog root, exact
   logical row IDs and provenance, newest-version visibility and tombstones.
2. Disjoint placement tags must demonstrably map to distinct physical primaries;
   client count and hash-slot count alone are insufficient evidence.
3. Each shard-local batch has bounded rows/bytes and an atomic ownership, lease,
   registration, complete-image and durable outcome check. Uncertain outcomes
   are resolved by reads; input and unknown mutations are never resent.
4. A table-wide publish cannot expose partly prepared partitions. Shard-local
   retention must protect outputs before global visibility and protect every
   reader until release. Neither a client clock nor cross-slot Lua/MULTI provides
   a distributed atomicity or retention proof.
5. Cancellation, coordinator loss, network partitions, failover, membership
   changes, Search readiness, restore and GC require explicit bounded outcomes.
6. A different physical layout needs a new storage protocol/capability fence.
   Protocol-17 backups remain protocol 17; no implicit conversion/migration.
7. Query execution resolves visibility before predicates and merges globally
   before aggregate finalization, DISTINCT, ORDER BY, OFFSET and LIMIT. All 43
   canonical ClickBench queries require independent complete-result validation.

## Acceptance status

The design and dependency assessment are delivered. No distributed implementation,
profile or multi-primary Software/Flex test is claimed. Approval of a supported
coordination contract and the implementation/validation phases in [plan](plan.md)
remain gates to completing the full issue acceptance criteria.
