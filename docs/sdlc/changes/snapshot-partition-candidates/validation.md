# Validation

2026-11-24; base main `14fcd545`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Regression

`go/snapshot_partition_candidates_integration_test.go` creates a 2,000-row managed
table and modifies it:

- `g = 'hit'` for every fourth row; NULL `v` for every tenth;
- UPDATEs that stop one match, start another and change a matching row's value;
- a DELETE.

Each query is planned into four partitions on one connection. The partitions are read on
another connection with hooked maintenance and execution clients. Rows are compared with
the unpartitioned result.

| Query | Original `snapshot_partitions.go` | Fixed |
|---|---|---|
| `WHERE g = 'hit'` | FAIL: 0 of 4 filtered partitions read span-bounded candidates | PASS |
| `WHERE g = ?` (bound) | FAIL: 0 of 4 | PASS |
| `WHERE g = 'hit' AND k % 3 = 0` | FAIL: 0 of 4 | PASS |
| `WHERE g = 'hit' AND v IS NULL` | FAIL: 0 of 4 | PASS |
| unfiltered | PASS, no row-ID Search queries | PASS, no row-ID Search queries |
| `WHERE k % 4 = 0` | PASS, no row-ID Search queries | PASS, no row-ID Search queries |

Rows were already complete before the change; the defect is cost.

## Tests

All runs used the dedicated `tpch-599q` fixture described below.

**Targeted ordinary suite.** `go test -count=1 -p 1 -run
'Partition|SnapshotCandidate|SnapshotPrefetch|SnapshotReadiness|SnapshotNumericNullOrder|SQLStoredRowInspection|IndexReadiness'`
with `REDIS_URI`: 86 passed, 37 skipped, 9 failed. The failures came from the run
environment, not this change:

- Seven `TestRetainedPartitionAPI*` tests failed because this direct run lacked the
  verified retained-control declaration.
- `TestLiveHashPartitionIndexDefinitionChanged` and `TestPartitionManagedCopyOwnership`
  timed out waiting for application indexes. Their initial scans also covered the
  800k TPC-H HASHes still loaded in the database.

**Snapshot and failure reruns.** These ran through `software-setup.sh --exec`
(`SOFTWARE_SHARDS=1`). The script reverified the reused database and active 4× QPF,
then exported `REDIS_RETAINED_CONTROL_PERSISTENCE=guaranteed`.

- **Snapshot partition suite** (`REDIS_SNAPSHOT_URI`, disposable database): 27 passed,
  8 skipped.
  - Passed: the `TestSnapshot*Partition*`, `TestSnapshotDirect*` and
    `TestSnapshotCandidates*` families, plus directory-aggregate and tuple tests.
  - Skipped: one needs a cluster URI, six need the reader-lease-model gate, and one is
    an opt-in profile.
- **Earlier failures:** all 9 passed on a rerun, as did the new regression. 4 skipped:
  two helpers, an `ACL SETUSER` refusal on Software and a policy fixture.
- **Unmanaged partition suite** (`REDIS_SNAPSHOT_URI`): 16 passed, 2 skipped (a helper
  and an opt-in profile).

**Skip reasons in the ordinary run.**
- Software refuses `ACL SETUSER`: 3 tests.
- Required fixtures: a Search-config permission fixture (1 test) and an owned
  standalone policy fixture (1 test).
- Subprocess helpers: 5 tests.
- Opt-in profiles: 2 tests.
- The cluster URI and the reader-lease-model gate: 1 test each.
- `REDIS_SNAPSHOT_URI` gates: the remaining 23 tests. The snapshot and unmanaged suites
  above ran them.

**Unit-only pass.** `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...` exited 0.

## Third-path Q19 measurements

**Fixture.**
- Dedicated Compose project `tpch-599q`: Redis Software 8.8.0, one shard, 4 GiB RAM
  database.
- Verified active 4× QPF, `mnp`, 32 connections, `noeviction`, TLS.
- 10% TPC-H subset: `lineitem` 600,093 rows, `part` 200,000 rows; indexes complete.

**Client.**
- Linux ARM64 container in the server's network namespace, capped at 4 CPUs / 4 GiB.
- DuckDB 2.0.1 with 16 threads and the Query-farm/adbc_scanner PR #22 build,
  `query_pushdown false`.
- `scan_threads` N with `adbc.redis.partition_count` N; N = 1 uses the single-stream path.

**Runs.**
- Five rounds; each round runs every (build, N) once, rotating build order.
- 45 of 45 runs per matrix returned the DuckDB reference revenue `277320.2089`.
- Lineitem read is the span from the first partition start to the last partition end,
  taken from a scratch-only trace.

**Builds.** All from `14fcd545` with the same trace; Linux ARM64 sha256:
- base `cba23eb0a7dad1fb6301d09e84da625fbb7b1354e558be5b6dd637c8c0f909e1`
- wide pages `171abc0c96b2abdc82c707d0297d79aa66f6b70e94b94e9e5fc5be5cca2fe799`
- prototype candidates + wide pages `710486b581fa89701948ea42ce20948b5b3619e7248542f6bc83a087688ffb72`
- this change `b93e76b596a307d6db661273e0b6081cdee4a29751b860953a2b9531d27d0379`

### Variant matrix (21:48–21:50Z)

Medians, in seconds:

| N | Measure | base | wide pages | prototype |
|---|---|---|---|---|
| 1 | Q19 | 1.51 | 1.66 | 1.54 |
| 8 | Q19 | 1.84 | 1.91 | 1.03 |
| 8 | lineitem read | 1.41 | 1.47 | 0.44 |
| 16 | Q19 | 1.64 | 1.66 | 1.11 |
| 16 | lineitem read | 1.22 | 1.19 | 0.51 |

- **N = 1** is the same single-stream code in every build, so it measures noise.
- **Wide pages** gave no gain.
- **Prototype.** It also sent unfiltered `part` partitions through a pure row-ID Search
  range, slowing their read from 0.29 s to 0.43 s (median, N = 8). This change excludes that case.

### Change matrix (22:02–22:03Z)

Medians, with IQR, in seconds:

| N | Measure | base | this change | prototype |
|---|---|---|---|---|
| 1 | Q19 | 2.00 [1.62, 3.06] | 1.81 [1.63, 2.46] | 1.65 [1.34, 1.70] |
| 8 | Q19 | 2.03 [2.00, 2.19] | 0.70 [0.69, 1.13] | 1.36 [1.15, 1.46] |
| 8 | lineitem read | 1.61 | 0.40 | 0.60 |
| 8 | part read | 0.27 | 0.20 | 0.45 |
| 16 | Q19 | 1.91 [1.84, 2.39] | 0.76 [0.75, 0.78] | 1.13 [1.13, 1.59] |
| 16 | lineitem read | 1.40 | 0.34 | 0.62 |
| 16 | part read | 0.28 | 0.25 | 0.43 |

- **Overlap.** At N = 8 and 16, every run of this change (maximum 1.14 s) was faster than
  every base run (minimum 1.61 s).
- **Same output.** Every lineitem read emitted 150,019 rows in N batches.
- **Beats the prototype.** With an exactly pushed TAG filter the planner residual is empty.
  The candidate read therefore uses positional HMGET with 4,096-row pages, which the
  prototype's full-predicate residual disabled.

### Concurrent host load

The Docker VM (16 CPUs, 33.8 GB) also served other sessions' workloads:
- the `redis-adbc-flex` Flex SF1 investigation used 100–250% CPU, 14.4 GiB;
- another TPC-H fixture used 9–158%;
- that investigation's auto-named per-query harness clients (one per SF1 query, from
  21:58:56Z, each about 61 s) used up to 151%.

`docker stats` before and after each matrix, and monitor samples, are retained with the
scratch results. The first runs of a batch and part of the variant matrix's last round
were slower in every build; they coincided with those load spikes. Treat the
within-matrix comparisons as evidence and the absolute times as environment-specific.

### Not covered

- A full-scale SF1 run. #599's pending full-memory RAM fixture A/B covers the
  unpartitioned path.
- Redis Cloud and multi-shard topologies.
