# Implicit snapshot order without redundant reducers

Date: 2026-11-05. Implementation: c06e181; parent e04a87c. Status: focused
validation and matched native comparison pass; complete regression runs remain.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

An unbounded narrow SELECT without ORDER BY still constructs per-segment ordered
Search reducers and visibility masks to recover implicit insertion order. The
exact directory/candidate merge already produces that ascending row-ID order.
Besides extra work, the reducer can fail under a lowered aggregate cap that a
named principal cannot discover. The prior TLS probe with a real Software cap
of 137 reports `SEARCH_LIMIT_OVER` for such a query.

Extend the existing wide-read shortcut to every projection width for unbounded
implicit order. The normal exact visibility path keeps newest replacements and
tombstones ahead of predicates. Selective predicates retain bounded Search
candidates; unfiltered and residual scans retain authoritative directories.
No SQL replay, new option, storage format, relaxed index validation or changed
explicit ordering/LIMIT path is introduced. This does not resolve arbitrary
explicit sorting under an undiscoverable lowered cap.

A separate read-client hook refuses reducers while leaving the fixture writer,
server and immutable indexes unchanged. The pre-change unfiltered regression
fails in 0.25 s with the same typed cap error. After the change, the complete
300-row baseline plus replacement, tombstone and later insertion returns every
independently calculated row in implicit order. Indexed and residual predicates
are checked separately, including a replacement that no longer matches its old
predicate, exact BIGINT values beyond 2^53 and NULLs. No reducer may execute;
unfiltered/residual paths must read exact member directories. Four focused race
pass events, zero skips/failures, root 2.155 s. An initial post-change observer
looked for the wrong directory command; correcting it preserves the row oracle
and verifies the actual ZRANGE member reads.

A broader race selection passes 41 test events, zero skips/failures, root
13.047 s: implicit/explicit ordering, visibility, index aggregates, candidates,
prefetch, NULL-order proofs/fallbacks, readiness transitions, exact HASH slices
and Substrait visibility. These are scoped checks, not the complete suite.

The public native comparison harness adds a complete implicit-order read over
its existing replacement/deletion fixture. All results are fetched and checked
against independently calculated rows inside each timed sample. No performance
gain is inferred for other workloads from this change.

## Native cap reproduction

The same Software fixture installs a real aggregate cap of 137 and creates a
named planner with unchanged full query permissions. An ordinary unbounded
SELECT over the ingestion/deletion fixture fails with `SEARCH_LIMIT_OVER` using
the preserved 50d6b84 native library. Switching only the library to c06e181
returns all 1,099 expected rows, exact decimal/binary/NULL values and Arrow
schema. The remaining partition planner checks pass; the separate restricted
worker still fails permission checks. Both attempts remove their owned role
mappings and restore the original cap. No reader grant was broadened.

The new library is built with `make -C go build` at committed c06e181, SHA-256
`349a3cd6b28184085d25e52d2e53074705f4cb0d190bb10e9abdea2800bff040`.
The preceding 50d6b84 library is preserved unchanged with its recorded hash.
This closes the reproduced unbounded implicit-order cap case, not arbitrary
explicit sorting or restricted-principal snapshot authorization.

## Matched native reads

Six isolated processes run preceding snapshot 50d6b84, current c06e181, mutable
f4d5b78, then reverse order. Each process has the same 20,000-row fixture with
a replacement/deletion and 32 unrelated tables. Eight read cases each perform
one warmup and five measured samples. Every timed sample includes complete
Arrow fetch and independently checked values; the new implicit case also checks
its exact native schema. Setup is outside timing. No local builds, tests or
database probes overlap timed processes. This is synthetic load, not ClickBench.

Pooled medians from ten samples per case/build, milliseconds:

| Case | Snapshot 50d6b84 | Current c06e181 | Mutable f4d5b78 |
|---|---:|---:|---:|
| Implicit all 19,999 rows | 974.100 | 721.485 | 129.197 |
| Selective, no match | 14.300 | 13.413 | 6.663 |
| Matching selective | 15.875 | 15.685 | 4.346 |
| Grouped | 17.869 | 17.486 | 4.808 |
| Distinct | 21.555 | 16.060 | 4.923 |
| Top-K | 24.307 | 17.896 | 77.603 |
| Top-K NULLS LAST | 17.304 | 14.557 | 4.329 |
| Point | 14.722 | 11.544 | 5.891 |

Implicit-read median improves 1.350x (25.93% lower); its two current process
medians are 696.524 and 746.446 ms, versus preceding 1,075.571 and 868.596 ms.
Its current pooled p95 is 966.304 ms, versus preceding 1,215.997 ms. The current
median remains 5.584x mutable. Two processes per build and ten samples do not
establish statistical or general workload guarantees. Other cases remain listed
for completeness; changes to explicit ordering/aggregates are not attributed
to the implicit-order shortcut. [All samples and provenance](implicit-read-comparison.json)
retain each source, actual storage model/protocol, driver/harness hash and settings.

Benchmark Python 3.13.16, PyArrow 26.0.0, manager 1.13.0. Apple M5 Max, 18
logical host CPUs, 36 GiB host memory; Docker 18 CPUs / 25,421,033,472 bytes.
Hardware allocation is checked immediately after the six processes; no hardware
reconfiguration occurs during comparison. Complete current-source native and
race suites remain separate gates.

The subsequent complete native ADBC validation at c06e181 passes **565 tests
and three subtests**, eight explicit capability skips, 96.92 s; Python 3.14.10,
PyArrow 26.0.1, manager 1.13.0, exact library hash above. The skip reasons retain
unimplemented connection/ingestion capabilities, table-type filtering,
secondary-catalog schema and multi-statement transactions. After that process
terminates, the complete current snapshot race selection starts with the
preserved older native worker supplied. Its result is still pending; do not
count it or a fresh ordinary/platform suite as passed.

Owned Redis Software 8.8.0, strict TLS, active/effective 4x QPF, noeviction,
three primaries behind its proxy, macOS ARM64 / Go 1.27.3. Proxy tests do not prove
Cluster API routing. Full source-labeled suites and other outstanding parity
gates remain open; the preceding ordinary/snapshot/native results cover earlier
production source. Author review is not maintainer approval.
