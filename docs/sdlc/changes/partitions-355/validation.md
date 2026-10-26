# Partition API verification and review

Date: 2026-10-20. Author/reviewer: Codex. Human review: pending.
Source baseline: a08573bdc40c07bca5bc3ccf9cee1e59cbc6490c plus this working diff.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

## Checks run

All three local Redis targets use `redis:8.6.2`: standalone on ports 6379 and
16379, and a three-node OSS Cluster API deployment on ports 7001–7003.
Tests create isolated `it_partition_*` tables and an `it_acl_partitions` user;
they remove those fixtures afterward. No Cloud or Redis Software deployment
was exercised for this change.

| Check | Result |
|---|---|
| `REDIS_URI=redis://localhost:6379/0 go test -json -count=1 -p 1 ./...` from `go/` | All 3 packages passed; 865 test/subtest pass events, 11 skips, no failures |
| `REDIS_URI=redis://localhost:6379/0 go test -race -count=1 -p 1 -run '^TestPartition' ./...` | Passed, including boundaries below 2^53, aggregate cap regression, captured time zone, temporary-name shadowing and ACL revocation |
| `REDIS_URI=redis://localhost:16379/0 go test -count=1 -p 1 -run '^TestPartition' ./...` | Passed; read-only ACL success and denied Search command enforced |
| `REDIS_URI=redis://localhost:7001/0 go test -json -count=1 -p 1 -run '^TestPartition' ./...` | Passed; 15 test/subtest pass events and no skips |
| `make -C go build` | Native macOS arm64 shared library built successfully |
| New `go/validation/tests/test_partitions.py` against port 6379 | 3 passed, including C/Python schema and descriptor export/release, cross-process reads after planner close, retries, decimals/nulls/booleans, malformed/stale descriptors, and connection cancellation |
| Same native Python tests against cluster port 7001 | 3 passed |
| Runnable `python/examples/adbc_partitions.py`, 2 worker processes, port 16379 | Reported `Read 5 rows from 4 partitions`; test fixture removed |
| Python compilation, Go formatting, `git diff --check` | Passed |

The full Go suite's 11 skips are nine opt-in profiling fixtures, the Flex
deployment test (no `REDIS_FLEX_URI`), and the opt-in server-pause timeout test.
No partition tests were skipped on the standalone/cluster test environments.

The native Python tests used the existing temporary Python environment with
ADBC driver manager, PyArrow and pytest. Its full `adbc_drivers_validation`
dependency was unavailable, so pytest was invoked with `--noconftest` and a
small in-memory fixture plugin providing the built library path and Redis URI.
The three checked-in test functions ran unchanged. The complete upstream ADBC
validation suite was not run. In the normal configured validation environment:

```sh
cd go/validation
REDIS_URI=redis://localhost:6379/0 uv run pytest -v tests/test_partitions.py
```

Raw local Go evidence during this session: `/private/tmp/redis-partitions-355-go-tests.json`,
`/private/tmp/redis-partitions-355-cluster-tests.json`, and
`/private/tmp/redis-partitions-355-race.txt`. These are temporary local files,
not published CI artifacts or release evidence.

## Review passes

Correctness/lifecycle: checked explicit SQL exclusions before planning, exact
numeric range boundaries, empty/sparse partitions, no global-order claim,
Arrow schema equality, reader ownership and cursor cancellation/release.
Independent worker connections consume descriptors after planning handles close.
An initial Search wildcard-composition failure was fixed; coverage tests passed.

Security/ownership: descriptors contain query literals but no endpoint/password
or retained pointers. All queries are reparsed and restricted; worker ACLs and
physical identity are checked. Fixed a review finding where direct-key
predicates could skip the cap probe before conversion to Search range cursors;
the regression covers both direct-key and ordinary predicates. Physical
fingerprints omit comments, defaults and mutable index-value proof caches so
ordinary metadata maintenance does not invalidate a read plan.

Intent/evidence: implementation is deferred range reading rather than retained
snapshots. No bound parameters, projection expressions or global SQL operations
are claimed. Lowered Search aggregate result caps are rejected based on the
existing per-connection probe; managed services or restricted ACLs which hide
CONFIG must retain their standard unrestricted aggregate setting. Connection
cancellation owns Python partition reads, not cursor statement cancellation.

No remaining actionable finding was identified in this self-review. This is
not human approval. Release wheels, Linux native packaging, Cloud/Software
compatibility and performance graduation remain outside this local evidence.
Existing Dask working-tree changes were preserved and were not part of this
review. Nothing was merged, released or posted to the issue by this change.

## PR preparation

At the user's request, the implementation was copied into an isolated branch
based on `c737cb8` (current main, including the already-merged Dask integration).
The upstream changes did not alter the Go sources or Python option enum used
by this implementation. The focused partition race/integration suite passed
again in that checkout. The original working tree was left intact.

The user also requested follow-up issues for deferred scope. Those were filed
as #358–#363 and linked from the consumer documentation and PR description.
These are follow-up work, not additional compatibility or correctness claims.
