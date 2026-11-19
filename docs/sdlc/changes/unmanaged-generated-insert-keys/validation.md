# Validation

Date: 2026-11-18. Source base: `818063a8`; local change before commit.
Author: Codex. Status: focused verification passed.

`make -C go redis-up` succeeded. The `software-setup.sh --exec` gate verified
Redis Software 8.8.0, three primary shards, Search, RAM/noeviction and active
4x QPF with six Search workers, mnp routing and 32 connections before tests.
Operational connection artifacts remain private.

From `go/`:

```sh
bash software-setup.sh --exec bash -c 'export REDIS_SNAPSHOT_URI="$REDIS_URI"; go test -count=1 -v -p 1 -timeout=10m -run "^(TestUnmanagedLiveSourceWrites|TestUnmanagedEncodingAndMissingIndexDrop|TestUnmanagedForeignFilteredMembership|TestLiveGeneratedInsertKeys|TestUnmanagedArrowAppend|TestUnmanagedArrowRefusalsBeforeInput|TestUnmanagedArrowKeyChecks|TestUnmanagedArrowInputSchemaChecks)$" .'
```

Exit 0: eight top-level tests passed, no skips. Generated keys were checked for
canonical UUID v4 form and uniqueness, authoritative source values, absence of
internal HASH fields and absence of managed catalog publication. Adopted and
foreign registrations exercised multi-row VALUES, implicit target lists,
DEFAULT, INSERT SELECT and RETURNING. Existing explicit NULL, prefix, filtered
index and Arrow schema/key refusals passed. Existing live generated-key coverage
also passed. Two earlier development runs failed because the new implicit-list
fixture omitted its extra public field, then used foreign column ordering for
adoption; the fixture now uses each registration's actual public column order.

Formatted changed Go files; `git diff --check` passed. Full suite, cluster
matrix, Flex and performance tests were not run.

Self-review covered SQL default evaluation, explicit NULL handling, retained
source ownership and no-replay boundaries. No actionable findings remain from
this review; human PR review remains pending.

See [intent](intent.md), [specification](spec.md) and [plan](plan.md).
