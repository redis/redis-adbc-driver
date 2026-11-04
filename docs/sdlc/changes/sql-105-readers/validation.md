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
