# Validation

Date: 2026-11-20. Author: Codex. Status: focused checks passed.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

Dedicated Redis Software fixture created with `make -C go redis-up`, using private
Compose project/ports and an explicitly allocated private network because Docker's
default network pool was exhausted. No unrelated fixture was changed or pruned.
Setup verifies active 4x QPF, six Search workers, mnp routing, 32 connections,
three-shard Redis Software 8.8.0, TLS and noeviction. Credentials, endpoints and
operational artifacts remain private.

Verified commands (dedicated private Compose settings supplied in environment):

- `make -C go test-software TEST_ARGS='-run ^TestLiveEvolution'`: five top-level
  evolution tests passed, zero skips. Source preservation covers exact BIGINT,
  binary row keys, DUMP bytes, absolute expiry, unknown fields, selected schema,
  SQL rename, removed fields and index policy. Invalid values fail before staging.
- `make -C go test-software TEST_ARGS='-run ^TestLive'`: 27 top-level live/CLI
  tests passed, zero skips. Existing SQL writes, isolation, Arrow creation/append/
  replacement, schema and lifecycle regressions remain covered.
- `make -C go test-software TEST_ARGS='-race -run ^TestLiveEvolution\|^TestLiveCLI$'`:
  five evolution cases and the expanded CLI preview/apply evolution test passed,
  zero skips and no race reports. Native CREATE/DROP permission refusals and
  post-DROP reply loss are injected hooks, not a newly provisioned limited ACL
  principal. Each mutation is submitted once; the retained state permits explicit
  safe cancellation/resume. Unknown unobserved CREATE identity remains reserved.
- `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  all packages passed. Integration skips expected; this is unit/build coverage.
- `gofmt` and `git diff --check` passed. Documentation links/anchors reviewed.

The initial focused run caught stale rename mappings for an unselected removed
field; mappings are now restricted to selected source fields. No server or source
assertions were weakened. Full integration suite, independent Cluster API,
Flex, packaged native wheels and performance tests were not run. Live mode remains
unsupported on Flex; no deployment or performance support is inferred.

Self-review covered exact types/NULL handling, ownership and namespace isolation,
CAS races, command and pipeline uncertainty, index readiness and cleanup, and
cancellation. New SQL remains blocked until transition cleanup completes; external
writers/administrators must honor the explicit quiescence requirement. No
unresolved actionable self-review findings; human PR review remains pending.
