# Validation

2026-11-03. Base `6d7ac409d8c9f11163b4f4cec63e2d4da00e69f2`.
Author: Codex. Local verification complete; draft for maintainer review.
[Intent](intent.md), [specification](spec.md), [plan](plan.md).

Baseline reproduction on Redis Software 8.6.2, three shards, TLS, Search and
verified active 4x QPF: a streamed SELECT of 25,000 materialized-view rows failed
after 10,000 rows when another connection ran REFRESH CONCURRENTLY. It reported
the driver's generation-change I/O error. The regression now validates all old
row values while new queries read a different, smaller published generation.

`make -C go test-software` at `169e410` passed across all five packages:
**830 top-level tests passed, 40 skipped, zero failures**. The driver package
completed in 252.626 seconds. This includes the seven new reader lifecycle,
multiple-generation, abandoned-owner, renewal, Open-recovery and publication-race
regressions, plus TRUNCATE rejection. Existing skips cover Software ACL/command
introspection restrictions, opt-in profiles and external/environment probes.

Self-review then corrected an overly broad lease shortcut: a valid reader lease
must not bypass the existing generation-ownership fences for destructive DDL.
Retained capture also continues checking layout changes within a generation.
Added a focused ownership-fence regression. After this correction:

- Unit-only suite (`env -u REDIS_URI go test -count=1 -p 1 ./...`): all five
  packages passed; integration skips in this run are expected.
- Software race run (`go test -count=1 -race -v -p 1 -run
  'Test(Matview|SQLNamedIndexAndMatView|SQLNewFeatureErrorsAndMetadata|SQLAutoIngestOwnershipAndFailedRefresh)' .`
  through `software-setup.sh --exec`): **11 top-level tests passed, zero skipped,
  zero failures**, including the ownership-fence regression; no races reported.
- Native library (`make -C go build`), `git diff --check` and change-record
  relative-link checks passed.

The full-suite result precedes that small fence correction; the final correction
was verified by the unit/race/build checks above, not another full-suite run.
Earlier interrupted full runs are not claimed as passing. No human approval or
hosted CI result is asserted by this local validation record. #105 stays open.

dbt adapter work is tracked separately in redis/redis-dbt-adapter#38 and #39.
No complete dbt run, release or cross-platform validation is claimed here.

## PR #289 repair, 2026-11-05

Pulled head `715d7a7` and merged main `a9f3e41`. Resolved conflicts in
`partitions.go` by preserving result-layout selection and the leased sequence
bound, and in `transport.go` by joining background workers before releasing
generation leases.

The failed hosted runs (`41770965525`, `41770965528`) reported restricted
connections failing during Open because retirement recovery required HSCAN.
Reproduced both `TestACLDriverUser` and `TestACLReadOnlyUser` failing with that
NOPERM error on an isolated Redis OSS 8.6.2 authenticated TLS fixture.
Retirement enumeration now uses the existing HGETALL metadata permission.
Open treats garbage collection as best effort, retaining retirement records
for an authorized retry. Added `TestACLMatviewRetirementOpenRecovery` to verify
that restricted Open succeeds with abandoned work and an authorized Open later
collects it. Existing permission assertions were preserved.

Validation of the repaired code on macOS arm64:

- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages passed;
  integration tests skip without an endpoint.
- TLS OSS focused ACL, reader lifecycle, partition-reader and statistics
  permission tests: **25 top-level tests passed, zero skips/failures**.
- TLS OSS focused reader-generation and SQL lifecycle tests with `-race`:
  **12 top-level tests passed, zero skips/failures**, no race reports.
- Full `software-setup.sh --exec go test -count=1 -p 1 -json ./...` on a
  dedicated Redis Software 8.8.0 fixture with three shards, TLS, Search,
  noeviction and management-verified active 4x QPF (six Search workers, mnp
  routing, 32 connections): **905 top-level tests passed, 47 skipped, zero
  failures** across all packages. The driver package took 253.079 seconds.
  Skips cover Software protocol ACL/command introspection restrictions,
  opt-in profiles and external/environment probes. The new restricted-ACL
  regression passed on OSS and skips on Software, which blocks ACL SETUSER.
- `make -C go build`: native shared library passed.
- `git diff --check` and change-record relative links passed.

These are local results; hosted CI was not rerun and no release, dbt or
cross-platform result is claimed. The PR remains a draft and #105 stays open.
