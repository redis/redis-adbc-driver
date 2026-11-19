# Plan: narrow unmanaged aggregate reads

Date: 2026-11-18. Source: `b06aea4f`. Status: in progress.
See [intent](intent.md) and [specification](spec.md).

## Implementation

1. Reproduce complete-image retrieval on the four reported query shapes.
2. Select existing exact native projection reads for narrow wide-schema scans.
3. Stream single-column COUNT DISTINCT SQL keys into the existing bounded merge.
   Reserve bounded aggregate page-fetch workers through shared admission and
   join all native requests before cursor cleanup and token release.
4. Verify semantics, source ownership, cancellation, failure and temporary cleanup.
5. Run unit tests and focused Software race tests; review correctness/lifecycle,
   ownership/security and intent/evidence against REVIEW.md.
6. Run a separately labeled full-scale retest without overlapping other
   timed queries. Preserve deadline, memory cap, complete fetch and independent
   reference validation. Close only issues actually demonstrated resolved.

## Evidence so far

- Dedicated disposable three-shard Software fixture created by `make -C go
  redis-up`; the setup verifier checks active scaling factor 4, effective Search
  workers and routing, noeviction, TLS and RESP3. Private configuration and
  infrastructure identifiers are excluded from these records.
- The initial 1,001-row version of all four projection regressions fails
  against original query.go via Go's source overlay: each still transfers complete
  105-column HASHes. The final fixture spans 32,001 rows to cover the larger
  discovery-page boundary; the original failure evidence remains scoped to its
  initial test revision.
- The same four projection regressions pass with the narrow source selection.
  The fixture contains NULLs, exact BIGINTs above 2^53 and binary-safe strings.
- Final full unit suite passed: `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI
  go test -count=1 -p 1 -timeout=30m ./...`.
- Broad Software race selection passed 330 tests/subtests, covering unmanaged
  sources, typed aggregation, spill, exactness and worker-budget lifecycle.
  Seven opt-in diagnostic profiles skipped; four unmanaged replacement ACL
  subcases skipped because denied-permission operator principals were absent.
- Final focused Software race checks passed after adding public complete Arrow
  fetch and bounded-grant/released-worker assertions. Shared-library build via
  `make -C go build` passed (Go 1.27.4, macOS arm64).
- Focused Software race checks passed for projected reads, exact DISTINCT across
  spill boundaries, complete Arrow consumption, temporary cleanup and disk errors.
- Deterministic parallel-reader race tests pass: exact row order/values and
  later complete images after NULL vectors; unknown native outcomes cancel and
  join all siblings, preserve the original error and never replay requests.
- A synthetic 100,000-row / 105-column Software fixture on a macOS client over
  a Docker-published TLS route produced matching complete outputs for all four
  shapes. Pinned Go execution with parallel projected reads took 0.508–1.175 s;
  the complete-image source took 3.409–3.460 s. These are instrumented local
  synthetic results, exclude planning/Python/C-boundary costs, have no cold-cache
  claim, and are not full-scale ClickBench acceptance evidence.
- No 100M-row improvement or original-query resolution is claimed yet. A
  separately owned full-scale host is importing the existing compatible backup.
  This change requested no new full-scale ingestion or source export.

## Validation steering

The user authorized the retained RAM reproduction for these issues. Full-scale
validation uses a separate c8g.metal-48xl (192 vCPU / 384 GiB), with the original
Software image digest, Ubuntu image, three-shard 280-GiB database, 500-GB gp2
root volume and same-host Linux loopback Docker-published TLS route. Effective
4x QPF (six Search workers, mnp/32), noeviction, RESP3, compact HASH configuration
and disabled swap were verified before restore. Full-scale data must come from
a complete, compatible protocol-17 backup, with all exact object versions and
actual downloaded bytes checked before native import. No new ingestion is
requested. Infrastructure ownership, cost/expiry controls, restore journals,
cleanup receipts and other operational artifacts are retained privately.

## Page-size refinement

The initial candidate used 4,096-key pages and up to five fetch workers. A
separately labeled Go overlay prototype on the same local 100K synthetic fixture
used up to 32,000 keys per scalar page and yielded matching complete outputs:
projected execution 0.274–0.353s versus 3.397–3.542s for complete images. This
motivated the bounded larger-page follow-up; it is not full-scale acceptance
evidence. The implementation additionally scales pages by fetched-field width,
respects shared worker admission, and retains grouped/streamed page sizing.
The final full unit suite, broad Software race selection and macOS/Linux arm64
shared-library builds passed for this refinement at candidate `b1a535fb`.
The compatible full-scale backup has now been restored and typed readiness
passed. The first Q3 trial still exceeded 300 seconds; see the
[full-scale evidence](../../../../benchmarks/unmanaged-wide-aggregates/2026-11-18/README.md).

## Full-scale follow-up

The first `b1a535fb` Q3 trial consumed 76,608,000 rows by its last 299-second
sample, then hit the unchanged 300-second deadline. The supervisor stopped;
Q4–Q6 remain untested in this attempt. All four issues remain open.

Eligible scalar consumers now decode authoritative native vectors directly,
avoiding one transient HASH map per successful projected row. Native all-NULL
and ACL fallbacks still retain the complete later HASH image, while other
projection users retain map results. Reader worker joins, outcome checks,
page order, cancellation, cursor cleanup and shared admission remain in place.
Validate exact vector/map equivalence, schema binding and existing fault paths,
then rebuild the Linux arm64 candidate and repeat the original bounded trials.

Follow-up full unit suite and broad dedicated Software race selection passed
(Go 1.27.4, active verified 4x QPF); Linux arm64 build/full-scale proof pending.
