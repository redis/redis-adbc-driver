# Flex empty-tag compatibility: validation

Date: 2026-11-21. Base: `139350b7`. Status: bounded workaround validation;
full-scale crash avoidance and successful 100M publication remain unverified.
See [intent](intent.md), [specification](spec.md) and [plan](plan.md).
Implementation: [PR #572](https://github.com/redis/redis-adbc-driver/pull/572).

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
documents afterward. Their HASH values had not changed. The omission case
wrote a nonempty document first, while this failing case wrote the empty
document first: these observations do not isolate every ordering condition.
They capture the old option's correctness defect and a successful omission
scenario; they do not establish an executed original-order crash contrast.
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

## Canonical indexed pilot

The Linux ARM64 shared library built from `420c9819` passed a one-writer
canonical 105-column pilot using all 2,013,006 rows in the first four original
Parquet row groups. The DDL was unchanged: 77 NUMERIC and 28 TAG public
attributes remained present, with no public-field `NOINDEX` rewrite. Both
native indexes reported zero `INDEXEMPTY` attributes. The client used the
server's Linux network namespace, a 4 GiB limit and 4,096-row conversion
batches. The source Software Flex fixture had a 32 GiB logical capacity,
1 GiB RAM cache, 8 GiB container limit and verified active 4× QPF.

| Observation | Result |
| --- | ---: |
| Source input completion, stream clock | 173.243486 s |
| Acknowledged publication, load clock | 930.872680 s |
| Published rows / final native indexed documents | 2,013,006 / 2,013,006 |
| Native indexing failures | 0 |
| Independent validation | 325.669304 s |
| Whole case, including setup and joined cleanup | 1,258.694534 s |
| Peak effective server working memory | 5.082 GiB |
| Client process peak RSS | 603.14 MiB |
| Observed OOM / OOM-kill events | 0 / 0 |

Validation streamed all native logical IDs and checked count/uniqueness and
schema, then compared five actual native-ID point samples across all 105 typed
fields against independent DuckDB source casts. This is not an all-values check
of every row. No external ID sort was needed. Data and the server were retained.
Raw cgroup memory reached its 8 GiB cap while reclaiming cache, with 7,045
`memory.events.max` increments; the effective-memory guard, PSI and OOM checks
did not alarm. Footprints and raw charges are not interchangeable with
unreclaimable working memory.

An earlier attempt on the disposable fixture reached its unintended 4 GiB
logical quota during input, before final indexing. Its abort/nonpublication
was verified before that owned fixture was recreated. The successful attempt
explicitly verified 32 GiB logical capacity before consuming input; the failed
attempt's records remain private and are not relabeled as a code regression or
memory-pressure result.

This is bounded functional evidence, not an A/B speed comparison or completed
100M result. The input, index/seal/publication and independent validation
intervals must not be presented as one ingestion-only phase.

Provenance SHA256:

- Linux shared library:
  `9038952254b5b43d79e8b5076125d627cb1099cf9b070c19e84a68071ab081d8`
- Canonical DDL:
  `42d28575fd59fb4ae5d556683323c17937daaae8ff800e9089ddaaf91d1b2be7`
- Complete source Parquet file (the pilot reads its first four groups):
  `a390f6cb782f6aaef278c72fc1dd86c4f30bc843ebab3c159e9bd4d45ddb079f`
- Frozen private pilot harness:
  `e9dd8cfc11c104775fa968a895ffc19e71636ab806354b273507d009a8399d77`

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
