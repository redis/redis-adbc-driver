# Complete local ordinary run and isolated snapshot selection

Date: 2026-11-04. Go source **815c355**, unchanged throughout both commands.
Full [parity](plan.md) remains incomplete. [Merged contracts](upstream-merge-validation.md).

## Local commands and actual scope

Owned exclusive Redis Software, observed Redis 8.8.0, three primary shards,
strict data TLS, noeviction and active effective 4x QPF. `make -C go redis-up`
verified effective policy before the complete ordinary command; test-software
verified it again before both selections. Database-mutating processes ran
sequentially. The owned endpoint/container was checked before the initial reset.
No Go source was edited while either process ran. macOS ARM64, Go 1.27.3.

The complete ordinary command is:

```
make -C go test-software TEST_ARGS='-race -json -failfast'
```

REDIS_SNAPSHOT_URI is unset for this command. It exits zero: **1,938 test pass
events, 272 explicit skips, zero failures; root package 1,419.357 seconds**.
The CLI, benchproxy, integer predicate, test-environment and TLS-trust packages
also pass. CUDA has no applicable tests in this host build and is skipped.
This is one complete ordinary command, not a combination of continuations.

The separate disposable snapshot selection is:

```
make -C go test-software TEST_ARGS='-race -json -failfast -run Snapshot'
```

It sets REDIS_SNAPSHOT_URI to the same owned fixture only after the ordinary
process is terminal. It exits zero: **329 test pass events, 21 explicit skips,
zero failures; root package 374.706 seconds**. An AST call-graph scan identifies
152 families that directly or transitively use REDIS_SNAPSHOT_URI. All 152 are
included in the actual runtime list of 201 families matching Snapshot. The
selector also includes ordinary snapshot-lifetime/statistics tests and pure
unit guards; it does not assert every matched test needs the disposable endpoint.

These totals include parent/subtest events, not distinct coverage requirements.
All skip names and process/package completion records remain in private logs.
Ordinary skips include the disposable protocol endpoint, management/ACL and
command-counter restrictions, optional profiles and additional deployment/model
fixtures. The isolated selection's 21 skips are nine dedicated Cluster fixture
families, six staged isolated-reader model cases and six permission-preflight /
recovery events requiring separately provisioned principals. Those capabilities
are not established by skips. No existing skip was broadened.

The upstream NULL-statistics fixture now records expected NULL counts per DML
phase: assigning a non-NULL value to both matching rows removes the earlier NULL.
A hardcoded one-NULL expectation after that UPDATE was not an independent oracle.
Pinned row/column statistics and immutable native-input rejection are validated
by the separate complete selections in the merge checkpoint.

## Hosted evidence contradicts a platform-green claim

The PR's 815c355 check rollup returns 14 records: six success, five failure,
three skipped. Named successes include native Ubuntu, distribution, coverage,
helpers, Superset and DuckDB native checks; their names do not establish the full
client/server/platform matrix. No all-platform or release-green claim is made.

- The Linux AMD64 and ARM64 release jobs both hit Go's **ten-minute package
  timeout**. Their full commands in release.yml lacked the 30-minute clock
  already present in Make and test.yml. The same complete local root needs
  1,419 seconds. This change gives both release full-package commands and the
  macOS wide-fetch selection the existing 30-minute budget; per-operation
  deadlines, assertions, selected packages and configured skip input are unchanged.
  [Release workflow run](https://github.com/redis/redis-adbc-driver/actions/runs/41931526878).
- The macOS compatibility job also reports restricted-principal EVAL permission
  failures, unexpected HMGET command-counter reads, a writer-progress timeout
  and the same aggregate package timeout. Restricted readers remain an explicit
  parity gap. Counter failures need metadata/payload disambiguation; timing or
  permission assertions are not weakened here. The timeout edit alone does not
  fix those failures. [macOS job](https://github.com/redis/redis-adbc-driver/actions/runs/41931526878/job/126929344147).
- The AMD64 three-shard Software job fails TestSQLCommentConcurrent while
  reserving a workspace at the unchanged **16-generation limit**. Admission,
  retirement and fixture-owned references require investigation; no quota is
  raised and no SQL replay is introduced. The matrix label does not prove a
  direct Cluster API or multi-node recovery run.
  [AMD64 Software job](https://github.com/redis/redis-adbc-driver/actions/runs/41931526861/job/126929447025).
- The ARM64 three-shard job reaches its real SQL SELECT grant fixture, where
  provisioning a restricted reader fails because it cannot EVAL snapshot
  initialization. Isolated reader leases and generation-stable grants are still
  unimplemented; blanket catalog-write permissions are not a fix.
  [ARM64 Software job](https://github.com/redis/redis-adbc-driver/actions/runs/41931526861/job/126929446857).

Ruby's YAML parser accepts the release workflow and diff whitespace checks pass.
The validation Python environment lacks PyYAML, so its first parser invocation
was a dependency/setup failure, not passing evidence. Hosted reruns on the budget
fix remain required; local success does not override hosted failures.

## Review and next work

Reviewed exact source/process boundaries, sequential fixture use, active QPF and
policy checks, selector coverage, explicit skips, individual versus aggregate
clocks and source-specific hosted outcomes. Author review is not maintainer
approval. Operational identifiers, credentials, logs and build artifacts remain
private.

A pinned mutable f4d5b78 native baseline was built privately with its own
versioned Make target and verified artifact hash. An execute-only diagnostic
harness is prepared, but no new timing or profiling results follow from that
preparation. Current matched-source/server read/write/storage comparisons,
sustained compaction, public process/input resume, reader/grant isolation,
current-protocol Cluster/failover/AOF/RDB and complete native/platform evidence
remain required. The new hosted failures are actionable remaining work, not
acceptable exclusions from the completion criteria.
