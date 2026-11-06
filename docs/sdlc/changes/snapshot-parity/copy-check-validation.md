# HASH-copy checklist and private storage allocation

Date: 2026-11-04. Parent 2394d1d. Production remains protocol 15.
Full [feature and performance parity](plan.md) remains incomplete.

## Reproduced behavior and correction

InspectHashes predicted a legacy mutable table prefix/index by reading name
counters, registries and candidate keys. CopyHashes instead initializes an
always-on snapshot catalog and allocates fresh private storage. The resulting
checklist falsely blocked copies when application keys occupied the old logical
destination prefix, or the source overlapped that old prefix.

The new regression reproduces both false blockers before the fix: three failure
events (two children and their parent), root package 1.627 seconds. It continues
past the checklist mismatch to complete real copies, verifies exact int64/NULL/
source-key values, checks private identities, drops/reclaims their generations
and proves the application HASHes and source Search index survive unchanged.
Thus the reproduction distinguishes report failure from actual copy failure.

The checklist now reports fresh private row storage and a private Search index.
It does not predict/reserve an identity or scan a legacy destination. The unused
name-counter/registry/prefix-count prediction helpers are removed. Actual copy
allocation, publication and ownership fences are unchanged. Logical view/target,
temporary-schema, empty-source, column, Search/Flex and memory checks remain.
Inspection is advisory; execution still performs its normal target/admission
checks. The driver reference's example matches the new report.

The old blocker fixture removes its unscoped legacy counter setup and checks
that these prefixes permit private copies, while preserving view and empty-source
blockers. Discovery exposed another unscoped fixture read; tablePrefix now uses
the existing pinned table inspection helper. It still returns one descriptor's
physical prefix, not the union of a table's row segments.

## Verification

Owned strict-TLS Redis Software, Redis 8.6.2, three primaries, Search, noeviction
and active effective 4x QPF verified before mutating selections. Suites run
sequentially on macOS ARM64. Raw logs, credentials and operational artifacts
remain private. No ClickBench run/reload or elapsed-time performance claim.

- Focused copy/check/discovery selection: seven race pass events, zero skips/
  failures; root package 4.049 seconds. It includes CopyHashes conversion failure,
  append/replace behavior and exact private-copy/external-ownership assertions.
- Explicitly disposable catalog-state selection: four race pass events, zero
  skips/failures; root package 2.801 seconds. Before catalog creation, inspection
  accepts the overlapping legacy prefix and leaves the catalog absent. The first
  real copy initializes snapshots, returns an exact integer beyond 2^53 and leaves
  its source HASH unchanged through DROP. The test uses REDIS_SNAPSHOT_URI and
  resets only the explicitly selected disposable database.
- Unit packages: 944 pass events, 832 expected integration skips, zero failures.
  Unit integration skips do not establish compatibility; a parent pass whose
  integration children skip is not a database pass.
- make -C go adbc-cli builds successfully. Formatting and diff whitespace checks
  pass. Storage format and native ABI are unchanged.

The subsequent remaining-test diagnostic starts after the previously failing
TestHashInspectCopyBlockers in the earlier ordered test list. It reaches nine
pass events, one explicit TestACLHashTools administration skip and two failure
events (a child and its parent), root package 2.882 seconds. It stops at
TestAlterWaitsBeforeMetadataCommit's unscoped metadata setup. Later selected
cases did not run. This is a partial selection, not full ordinary-suite success;
its counts are not combined with the earlier prefix to claim a green suite.

## Review and remaining work

Author review covers catalog-present/absent behavior, advisory versus execution
checks, fresh physical identities, source provenance/NULL/precision, preserved
external data/indexes, reference-aware reclamation and unchanged real blockers.
This is not independent maintainer approval. Full ordinary, restricted reader
ACL, current Cluster/platform/recovery, explicit process resume, sustained
compaction and broader comparative performance remain open.
