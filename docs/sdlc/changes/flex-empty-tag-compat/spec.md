# Flex empty-tag compatibility: specification

Date: 2026-11-21. Status: proposed implementation contract, not approval.
See [intent](intent.md) and [plan](plan.md).

- New plain Flex TAG definitions retain their separator and case sensitivity
  and omit `INDEXEMPTY`. Normal sortable RAM indexes retain their existing flags.
- The capability probe exercises the actual schema used by new Flex indexes.
- An empty derived TAG cannot be used as a narrowing Search candidate when its
  physical plain index does not guarantee empty postings. Keep the full SQL
  predicate and use authoritative reads. Application-owned indexes retain their
  separately discovered capabilities and are not modified.
- Managed Flex results must distinguish empty strings, NULL, whitespace,
  separator-only values and values containing NUL. Verify filtering, ordering,
  grouping and aggregates, plus updates, deletion and immutable-generation
  replacement/compaction.
- Keep NUMERIC row IDs, readiness/failure checks, exact directory cardinality,
  prefix proofs and warning-free indexed-row counts. No SQL/input replay, initial
  scans, publication bypass, storage-format migration or GC configuration change.
- Existing physical indexes are not rewritten on open. Opening an old layout
  is not a claim that its native storage-engine vulnerability was repaired.

The native comparison runs only on disposable Redis Software Flex with freshly
verified active 4× QPF. It must establish actual GC/compaction exercise; survival
without a confirmed cycle is inconclusive. Keep operational and internal research
artifacts private. Full benchmarks remain gated on a verified compatible path.
