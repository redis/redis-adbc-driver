# Validation and review

November 12, 2026. See [intent](intent.md), [spec](spec.md), [plan](plan.md)
and the [report](../../../../benchmarks/table-modes/managed-write-2026-11-12/report.md).
Baseline: pulled main `060915bd`; local merge `3a86d1a4` has identical source.

## Reproduction and behavior

The new cold/hot/flush regression was run with row/index call sites restored
to full-body execution: it fails because all three trials send four bodies
per script and zero digest attempts. Restoring the implementation passes.
The test consumes and validates every value/type/NULL/multiplicity in the
3K x 105 fixture; compaction has its own cold/hot/flush fixture with exact
BIGINT values and complete output validation.

The existing control-script refusal executor now also handles the existing
row batch, index notification and atomic compaction copy bodies. Each script
keeps its own per-store EVAL-only state. Hot execution owns the fresh argument
slice and changes only the script header; a definite refusal copies it for
one full-body attempt. Both retain NoRetry. The Lua bodies, keys/arguments,
row/byte limits, storage protocol, pipeline commands, index proofs and durable
row/compaction outcomes are unchanged.

Unit cases cover all three bodies with hot/cold operation, a native EVALSHA
denial, unknown initial/fallback replies and runtime errors. Existing control
cases retain cancellation, joined/wrapped-error and textual-lookalike checks.
Fault observers recognize the new digests while preserving actual wire
commands. Cache-miss/command-refusal probes do not count as row mutations.
The partial-compaction-copy injection still mutates the actual wire script,
so it exercises incomplete-image repair on the cached path.

## Environment and checks

Isolated fixture started with `make -C go redis-up`, then verified active 4x
QPF / six Search workers / mnp / 32 connections. Redis Software 8.2.6-25,
Redis 8.8.0 / Search 8.8.8, one 1-GiB RAM shard/noeviction, strict data
TLS/RESP3. The comparison uses Go 1.27.3 on a shared Apple M5 Max / 36-GiB
workstation; Docker provides 18 CPUs and about 23.7 GiB. Task tests and
profiles run separately from timing.

- Focused Software race checks, including cache/flush, row atomicity, lost
  replies and incomplete compaction: 81 test pass events, three conditional
  skips, zero failures. The initial focused run caught
  four observer failures because they recognized only full-body commands;
  observers were updated without weakening assertions, then the selection passed.
- Broader Software `go test -count=1 -race -json -p 1 -run
  '^(TestSnapshot|TestManaged|TestIngestColumn|TestArrow)' -timeout=20m .`:
  1012 test pass events, 80 conditional/opt-in skips, zero failures or races;
  root 454.920 s. Includes large compaction/row budgets, import failure,
  metadata boundaries, publication, visibility, leases and Arrow. Skips retain
  Cluster API, recovery orchestration, restricted-principal and opt-in profile
  prerequisites; they are coverage gaps.
- Dedicated owned Software ACL harness: all three race tests pass, no skips
  or failures (10.064 s). The EVAL-only operator appends and fully validates
  4K x 105 rows, manually compacts them and shows exactly one refused digest
  per batch script followed by full-body execution. Existing control EVAL-only
  and point-lease cases also pass. Effective QPF is checked before tests and
  after temporary principals are removed; both accounts were removed.
- Unit-only all packages, `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test
  -count=1 -json -p 1 -timeout=10m ./...`: 1364 test pass events, 1263
  live/conditional skips, zero failures. Skips are not compatibility evidence.
- Versioned shared-library build, `make -C go build
  VERSION=v0.0.26-dev.write-publication`: pass.
- Raw logs/profiles, runtime identifiers, credentials and license materials
  stay private. No historical benchmark artifact was overwritten.

## Measurements and review

The report separates script-header bytes, client-observed command time and
end-to-end elapsed time. Both published call sizes validate full results
without retries. The smaller headers do not establish a throughput gain.
No native C-ABI, four-writer, unmanaged, physical Flex or 100M ClickBench
rerun is claimed.

Self-review follows REVIEW.md: correctness/lifecycle (no uncertain mutation
replay, cancellation and unchanged durable outcomes), authorization/ownership
(exact native refusal classification, per-script EVAL compatibility, unchanged
keys/fences), and evidence/scope (complete fetch, bounded counts, distinct
call sizes, all trials retained, no latency claim). This is not human
maintainer approval.

All 42 timing trials and four controlled phase profiles are retained with
separate labels. The final diagnostic removes a copying observer and records
cached/control source hashes. For each final trial, row/index script-header
bytes are exactly 487,440 uncached and 3,200 cached. Full results validate.
Final medians are slightly slower with caching; no throughput gain is claimed.

Completeness/median/header arithmetic and privacy checks on sanitized artifacts
pass. Formatting, Python syntax and relative documentation links are checked
before publication. The only build artifact moved out of the working tree is
the task-created Go profiling binary; it remains private.

Cleanup: effective Software 4x QPF/topology/TLS/noeviction reverified after all
tests and measurements. Removed only the task-owned Compose project and its
network; absence verified. Removed its generated test environment file. Other
fixtures were left intact. Temporary ACL accounts were removed by their harness.

Implementation source: `896c4bef6897cd28c015816ee1d6a31d659ac0c8`;
[PR #410](https://github.com/redis/redis-adbc-driver/pull/410).
