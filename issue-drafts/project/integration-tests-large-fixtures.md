Target: redis/redis-adbc-driver — new issue
Title: Integration tests fail on large or shared fixtures (fixed 10 s cleanup SCAN and index-readiness deadlines, live-table capacity)
Labels: bug, needs-triage
---
## Problem

Several recent change records report integration failures that "fail identically on the pre-change tree". Each time they were set aside as fixture conditions:

- **`TestPendingCleanupAggregateAndPartitionInputs`** (`go/index_readiness_integration_test.go:182`): retained-store cleanup runs `SCAN <prefix>* COUNT 100` across the keyspace under a 10 s context (`go/partition_retained_store_integration_test.go:56-75`). With millions of unrelated keys it hits the deadline (`docs/sdlc/changes/tpch-flex-read-path/validation.md:15-19`).
- **`TestPartitionManagedCopyOwnership/application_index` and `TestLiveHashPartitionIndexDefinitionChanged`** fail with "still indexing". The helper waits 10 s for `FT.INFO` to report the index complete (`go/appindex_integration_test.go:36-46`). The record notes the initial scans "also covered the 800k TPC-H HASHes still loaded" (`docs/sdlc/changes/snapshot-partition-candidates/validation.md:35-42`).
- **Live-HASH suites** failed with "live table already registered or capacity reached" (`docs/sdlc/changes/tpch-flex-read-path/validation.md:20-25`).
- **A shared 8 GiB database** refused writes at connect time with OOM (`docs/sdlc/changes/tpch-snowflake-joins/validation.md:23-24`).

Versions: Redis Software 8.8.0, one shard, verified 4× QPF, TLS. Seen on main `12f46745` and on the pre-change tree `adcc3b0e`.

Expected: these tests pass, or skip with a named precondition, whatever unrelated keys the database holds.

Actual: they fail. Real regressions in these suites can then be lost among the expected failures.

Not verified: whether the index's initial scan costs time in proportion to the whole keyspace on this deployment.

## Desired behavior

- Cleanup and readiness waits scale with the test's own keys, or the test checks a precondition and skips with a clear reason.
- Live registrations that count against a capacity are released, or the test reports the capacity precondition.

## How to verify

Load about 1M unrelated HASHes into a disposable database, then run:

```sh
cd go && bash software-setup.sh --exec go test -count=1 -p 1 \
  -run 'TestPendingCleanupAggregateAndPartitionInputs|TestPartitionManagedCopyOwnership|TestLiveHashPartitionIndexDefinitionChanged' .
```

Success: all pass, or skip with a precondition message.
