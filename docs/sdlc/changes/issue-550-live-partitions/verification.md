# Verification and review

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex.
Base: `1d0d0b3b`; measured/last focused source: `f28dbc042bbcd4e28b243c6e8e3d33fe893d0782`.
Status: implemented and locally verified; human review and hosted CI pending.

[Intent](intent.md) · [Spec](spec.md) · [Plan](plan.md)

## Environment

Dedicated Redis Software RAM database, Redis 8.8.0, TLS/RESP3, noeviction and one
primary shard. `make -C go redis-up` verified active 4x QPF, six effective Search
workers, mnp routing and 32 proxy connections before tests. Initial three-shard
creation could not allocate resources; retrying the supported one-shard topology
passed the same gates. Client: macOS 26.7.3 arm64, Go 1.27.4, Docker published-port
route. Credentials, endpoints, infrastructure identities and raw logs are private.
No Community, Valkey, ElastiCache or Memorystore tests were run.

## Checks

| Check | Result |
|---|---|
| Unit-only: `env -u REDIS_URI -u REDIS_SNAPSHOT_URI -u REDIS_FLEX_URI go test -count=1 -p 1 ./...` | All packages passed; Redis-dependent cases skip without endpoints |
| Software: `go test -race -count=1 -p 1 -v -timeout=10m -run '^(TestLive\|TestPartition\|TestSharedScan\|TestResultLayout)' .` | Passed, 176 passing test/subtest entries, 28.874 s; includes shared layout fix and existing live evolution/MERGE/incremental paths |
| Disposable snapshot endpoint, separately: `go test -race -count=1 -p 1 -v -timeout=5m -run '^TestSnapshotPartition' .` | Passed, 8 passing entries, 8.843 s, zero skips |
| Final focused Software: `go test -race -count=1 -p 1 -v -timeout=5m -run '^TestLiveHashPartition' .` | Passed, 19 passing entries, 9.777 s; adds producer-close lifetime coverage |
| `go test -count=1 -p 1 -run '^TestResultLayout' .` | Passed; checked allocator balanced after cached proof failure |
| `make -C go build` | Passed with Makefile version linker flag |
| Opt-in synthetic serial/parallel profile | Passed all six complete-result oracles; 7.456 s total test time; [report](../../../../benchmarks/live-hash-partitions/2026-11-20/report.md) |
| `gofmt`, `git diff --check`, local Markdown targets | Passed |

Normal fixture tests used `REDIS_URI`; snapshot tests used the independently
cleared disposable `REDIS_SNAPSHOT_URI` and ran separately. No tests shared a
database concurrently with another package, profile or agent. These are Go ADBC
API checks; no new Python/C wheel validation run is inferred from the shared
library build.

Explicit broad-selection skips: `TestLiveHashPartitionProcessHelper` is launched
by its parent with subprocess options instead of running standalone;
`TestLiveHashPartitionProfile` is opt-in and was run separately;
`TestPartitionReaderPermissions` skips because Software manages ACL users outside
Redis `ACL SETUSER`; `TestPartitionRuntimeCapDiscovery` skips without its opt-in
cap override. Native permission/unknown/composed refusal paths are fault-injected
on the actual owned source transport. This does not establish new restricted-user
management provisioning or a full deployment/release matrix.

## Correctness and lifecycle evidence

Exact fixed-source coverage/values pass across seven concurrent connections and
three independent client processes. Fixtures include actual NUL/non-UTF-8 key
bytes, exact BIGINT beyond 2^53, full residuals, NULL projected fields on unknown-only
HASHes, nested-key exclusion, unchanged source dumps/TTLs/unknown fields, source
updates after planning and producer closure.

Registration replacement and explicit live schema/index evolution invalidate old
descriptors and buffered readers. Unexpected index schema changes, descriptor
tampering, expiry/release, cancellation/Close and exhausted key/work/page budgets
fail explicitly. Duplicate SCAN replies do not duplicate rows. Lost pipeline
replies, direct permission refusal and composed WRONGTYPE/reply-loss errors never
become successful partial reads or trigger source replay.

Review identified a real cached-layout gap: a 10 MiB fixed-size-binary result layout
split one native batch, and a second cached slice escaped after both live evolution
and native child-pin expiry. The retained regressions failed before the fix and
pass after it under the race detector. Shared commit `617061b7` adds a safe internal
cached-slice validator; fresh first slices keep existing proof counts, transformed
failed slices are discarded, source input is never advanced/replayed, and Arrow
ownership balances in the checked-allocator test.

## Review and limits

Agent self-review covered REVIEW.md lifecycle/correctness, ownership/security and
intent/evidence passes. The cached-layout failure, strict source-pipeline outcome
checks and wrapper ownership/error paths were addressed before PR publication.
This is not human maintainer approval.

Retained capture is explicitly unfinished, Flex still excludes live registrations,
and this evidence does not establish multi-primary Cluster API/topology-change,
Cloud or packaged-release coverage. Discovery is O(partitions × source scan);
logical work/key caps are not RSS/RESP decoder limits and HASH payload sizes remain
application controlled. Exact same-name/same-definition out-of-band index
recreation is outside the driver-owned-index contract. Performance trials do not
show a general speedup. Temporary test resources are removed after verification.

## Integration refresh after #560

Merged main `40ff7977` into the published task branch, preserving the measured
source and shared-fix ancestry. The only content conflict was the read-coverage
matrix footer; resolution retains implemented directory aggregates, explicit
live-source buckets and the separate unmanaged follow-up. The automatically
merged `query.go` change records full predicates for managed directory baselines
and is disjoint from the live partition route.

The combined tree passed endpoint-free all-package unit checks (root package
28.804 s), `make -C go build`, `git diff --check` and 18 local read-matrix link
targets. No endpoint or instance was recreated for this documentation conflict;
prior runtime/profile evidence remains labeled with its actual source above.
Hosted checks on the new merge head are pending.
