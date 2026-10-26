# Verification and review

2026-10-22. Author: Codex. Base: `6508626`. Local verification complete.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

Environment: local Redis Software 8.6.2, one shard; `make -C go redis-up`
verified active QPF 4x and Search before integration checks. Operational
artifacts and credentials remain private. No performance claims or benchmarks.

## Checks

- `make -C go redis-up`: Redis Software 8.6.2, one shard, active QPF 4x;
  setup also verified the fixture's noeviction policy through the management API.
- `REDIS_URI=redis://localhost:6379/0 REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed
  go test -count=1 -p 1 -json ./...` from go/: all three packages passed,
  1,325 passing test/subtest events, 39 skips. This declaration uses the verified
  management-plane noeviction policy; CONFIG does not expose it on the proxy.
- Expanded `TestTransportDeadlineBoundsBlockedCommandAndPipeline`: passed after
  the full-suite build. All 24 cases (RESP2/3, single/Cluster routing, finite/no
  read timeout, command/pipeline/cursor) returned within one second for a 150 ms
  caller deadline, preserved TIMEOUT/57014, reused the pool and observed single
  command attempts. The Cluster slots are supplied by the protocol fault fixture;
  this is client routing coverage, not a live multi-shard deployment claim.
- Regression reproduction: a temporary Go overlay substituted the original
  `6508626` timeout.go while retaining the new test. The RESP3 single-endpoint
  command case with a one-minute read timeout failed the two-second test
  deadline. Current implementation passes. Source files were not reverted.
- `make -C go build`: native library built with its version linker flag.
- Native full suite, using the existing Python 3.13 validation environment:
  `REDIS_URI=redis://localhost:6379/0
  REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed python -m pytest -q -ra tests/`:
  **382 passed, 7 skipped, 3 subtests passed**. This includes upstream statistics
  and the new C-stream schema/value/filter tests. Focused retained native
  replay/equivalence checks also passed after adding the slicing queries.
- `gofmt`, `git diff --check` and new-document relative-link checks passed.

The first full Go run exposed stale ACL documentation paths left by the README
split and retained-policy discovery failures. Tests now read the actual driver
reference with unchanged ACL assertions; the rerun used the verified policy
declaration. No assertions were weakened or skips broadened. `uv` was absent;
the available validation Python environment supplied the same test dependencies.

## Skips and limits

Go skips include protocol ACL-user creation unavailable on Software, optional
profiling/benchmark/worker probes, process-helper entrypoints, three-primary/TLS
fixtures, policy-admission opt-in, Flex rejection fixture and command counters.
The new real-user statistics ACL case skipped because Software disallows
protocol ACL SETUSER. Successful restricted-credential behavior is not claimed.
Native skips: unsupported UNIQUE/FOREIGN KEY constraints, secondary catalog,
catalog setting, catalog ingest and transactions. Exact statistics are tested
as NOT_IMPLEMENTED. No new live Cluster/TLS deployment or published wheel was
verified; no server-work termination or atomic source snapshot is claimed.

## Review

Self-review used all three REVIEW.md passes. Checked canonical schema/union
layout and Arrow release paths, null versus zero statistics, error propagation,
readiness/ACL boundaries, managed-table eligibility, bound slicing operations,
single execution before partitioning, context/socket timer races, pool reuse,
cursor non-replay and autocommit unknown write outcomes. No actionable finding
remains in the scoped diff. Remaining issue scope is explicitly listed in the
contracts and intent; this PR must not automatically close #476–#478.
Maintainer review is still required; this self-review is not approval.
