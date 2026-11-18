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
6. Coordinate a separately labeled full-scale retest without overlapping other
   timed queries. Preserve deadline, memory cap, complete fetch and independent
   reference validation. Close only issues actually demonstrated resolved.

## Evidence so far

- Dedicated disposable three-shard Software fixture created by `make -C go
  redis-up`; the setup verifier checks active scaling factor 4, effective Search
  workers and routing, noeviction, TLS and RESP3. Private configuration and
  infrastructure identifiers are excluded from these records.
- Four narrow aggregate projection regressions fail against original query.go
  via Go's source overlay: each still transfers complete 105-column HASHes.
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
- No 100M-row improvement or original-query resolution is claimed yet. The live
  full-scale host is running another chat's main-code baseline retest. No new
  full-scale benchmark or export has been started by this change.

## Validation steering

The user explicitly authorized reusing the existing loaded RAM reproducer
without a backup restore, under its original 300-second deadline and memory
cap. Keep work independent: do not message the other benchmark chat or overlap
its timed trials. The user also requested another Redis Cloud Pro instance,
sized for 100M rows; provisioning is a separate environment and any validation
there must carry its actual Cloud server and settings, not the original RAM
reproducer provenance. No database backup/export has been started.
