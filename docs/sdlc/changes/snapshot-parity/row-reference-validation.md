# Validation: descriptor-owned row references

Date: 2026-11-03. Parent implementation `272ac1a`. Status: implemented and verified
locally with a comparative small-write measurement; this does not complete the parity plan.

## Implementation

Storage format 9 moves data ownership and counters into the metadata graph.
Every immutable native table descriptor retains its segment IDs once, independent
of how many buckets alias it. Copying a native descriptor attaches those data
edges before replacing the source pointer. Descriptor SET validates all new
sources and old counters before changing the JSON and edges in the same fence;
the original JSON bytes are stored unchanged, preserving exact int64 metadata.
Failed SET NX does not replace references. Copies with missing source edges fail
before creating a target object.

Reservation starts with an empty owned-output list. Publication releases only
those temporary output references after metadata owns them; committed manifests
retain no catalog-wide data arrays. Durable compaction jobs retain bounded
source/output lists until retirement. Native-value GC releases descriptor edges
only after its final bucket alias disappears. A durable zero-reference data queue
admits bounded, resumable row/index/directory deletion; the deleting state blocks
new references and late writers. The data registry no longer expands the bounded
control catalog or its HGETALL enumeration.

## Regressions

- Before: one-row UPDATE read seven unrelated table descriptors. After: zero;
  unchanged table values remain correct. The existing zero-unrelated-copy check
  continues to pass.
- A private native copy remains the sole owner of source rows after its base pin
  stops, the current table is dropped, and the original native object retires.
  Aborting that workspace then reclaims its descriptor, rows and registration.
  Its owned-output list is empty, so aggregate base references cannot hide a
  missing descriptor edge.
- Repeated descriptor SET and failed SET NX preserve independently counted
  references. An unregistered source is rejected without changing native JSON;
  exact BIGINT query results remain unchanged.
- Independent accounting sums every native descriptor edge, including queued
  objects, plus staging/job references. It matches data counters across DML,
  COMMENT, compaction, TRUNCATE, DROP and pinned old reads.
- Prior format 8 joins earlier formats in the rejection test; no migration is
  introduced. Row-only scripts omit descriptor helper definitions.

## Evidence and remaining work

Owned Software proxy fixture: three primary shards, verified strict TLS, active
verified 4x QPF and noeviction. Suites run sequentially against the disposable
database. Native builds use the Makefile version linker flags.

- Final complete snapshot race run: **95 pass events, seven expected Cluster API
  skips, zero failures**, 117.321 seconds.
- Final all-package unit run: **894 pass events, 676 expected integration skips,
  zero failures**.
- Final native snapshot validation: 11 passed, 13.775 seconds.
- Final native IPC module: nine passed, 1.17 seconds.

Author self-review inspected copy-before-detach ordering, atomic edge changes,
lease fencing, idempotent outcome/retirement paths, explicit same-slot keys,
integer preservation and external ownership. This is not maintainer approval.
Independent-writer publication, bounded bucket construction, resumable large
statements, sustained compaction, full feature/platform and final Cluster recovery
checks, broader benchmarks and owned cleanup remain open. No full parity or
throughput claim is made.

## Comparative small writes

[Raw samples](row-reference-benchmark.json) compare `272ac1a` (format 8) against
`8c2e336` (format 9), built with identical version flags on the same macOS ARM64
host and owned Software fixture. Each process resets its disposable database.
One initial row, two initial segments, one warmup and five timed samples per
case; baseline runs before current at each catalog size. Background maintenance
remains active. Execution/publication and complete RETURNING fetch are timed;
independent complete-result validation follows each write outside the timer.
All validation passed; setup is excluded. These are local latency samples,
not throughput or pre-snapshot parity evidence.

| Unrelated tables | Case | Baseline median ms | Current median ms | Baseline/current |
|---:|---|---:|---:|---:|
| 32 | Metadata COMMENT | 65.14 | 16.98 | 3.84× |
| 32 | One-row UPDATE RETURNING | 78.67 | 34.19 | 2.30× |
| 32 | One-row Arrow append | 79.58 | 32.51 | 2.45× |
| 128 | Metadata COMMENT | 164.43 | 26.79 | 6.14× |
| 128 | One-row UPDATE RETURNING | 284.23 | 66.35 | 4.28× |
| 128 | One-row Arrow append | 289.64 | 91.08 | 3.18× |

Latency still increases with catalog size. This comparison does not isolate row
reference publication from all maintenance or metadata effects. Catalog-wide
compaction discovery and unbounded touched-bucket construction remain separate
costs to remove. Reproduce with `go/validation/snapshot_read_benchmark.py`,
`--writes --rows 1 --repeats 5`, and `--unrelated-tables 32` or `128`, using the
owned-fixture verification/reset flags. No ClickBench reload or run is involved.
