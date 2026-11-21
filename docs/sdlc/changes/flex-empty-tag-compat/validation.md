# Flex empty-tag compatibility: validation

Date: 2026-11-21. Base: `139350b7`. Status: bounded workaround validation;
full-scale crash avoidance and successful 100M publication remain unverified.
See [intent](intent.md), [specification](spec.md) and [plan](plan.md).

## Native defect and comparison

On a dedicated Software 8.2.6-58 / Redis 8.8.0 / Search 8.8.8 Flex fixture,
active 4× QPF, six workers, mnp routing, 32 connections and Search on Flash
were verified before testing. The server had an 8 GiB limit and 1 GiB RAM
cache. The original benchmark and failed full-load fixtures were not used.

A small native comparison exercised actual GC. Without `INDEXEMPTY`, GC
completed, the observed cycle count advanced from zero to one, native
nonempty/all-document membership stayed correct for that physical schema,
and the empty HASH value remained intact. Native empty-tag lookup was
unavailable as expected. The SQL directory reader must provide its semantics.

With `INDEXEMPTY`, an empty-first case returned only the nonempty document for
a nonempty TAG query before GC, but returned both the empty and nonempty
documents afterward. Their HASH values had not changed. The captured native
comparison therefore demonstrates an empty-term correctness defect and its
avoidance; it does not demonstrate an executed original-order crash contrast.
That follow-up fault test was not completed. Raw evidence and operational
details remain private.

## Driver checks

- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_FLEX_URI go test
  -count=1 -p 1 ./...`: passed. Native integration tests without their endpoint
  variables skip; this result alone is not Redis compatibility evidence.
- Focused native tests on the verified Software Flex fixture: **15 top-level
  tests / 57 including subtests passed, zero skipped or failed**. They covered
  the new empty-value lifecycle, capability-probe cleanup, existing Flex
  lifecycle, Arrow ownership/modes, publication and index failure cases,
  compaction/recovery, partitions/rekey, and unmanaged source behavior.
- The new lifecycle test preserved exact empty/NULL/whitespace/separator/NUL
  values through Arrow append, SQL predicates and aggregates, updates/deletes,
  an actually published compacted index, and Arrow replacement. Native
  `FT.INFO` confirmed that public TAG attributes remain present without
  `INDEXEMPTY`, SORTABLE or UNF.
- Unit regressions cover empty-derived TAGs and union predicates, normal RAM
  flags, application index capabilities and hidden provenance schema generation.
  NUL fixtures construct actual zero bytes explicitly.

These driver checks ran on a macOS ARM64 Go 1.27.4 client using the Software
database's published TLS port. They are correctness checks, not a comparison
with Linux network-namespace benchmark timings. Formatting and diff checks
passed. A separate agent reviewed the production diff and test expectations;
that is not human maintainer approval.

## Boundaries

Existing physical indexes are not migrated, and their storage engine is not
repaired. Legacy plain-index read compatibility is supported by the existing
exact-directory source path; a separate cross-version native restore was not
performed for this change. Normal RAM index policy is unchanged, while the
focused native compatibility run used Flex. Application-owned indexes retain
their own declared capabilities.

No 100M load or reusable-backup success is claimed. The failed full run remains
recorded under its actual earlier driver and native index flags. A future
canonical indexed load also needs an adequate operation deadline; changing a
future deadline cannot extend a running statement. Do not replay an unknown
ingestion outcome or treat absence of a crash without confirmed GC as a pass.
