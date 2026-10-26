# Project context and evidence

Snapshot: 2026-10-20, source commit `6f42461`. This is a reading guide to the
checkout; links to live files may change after that commit.

## Product and boundaries

The driver provides SQL queries, Arrow result streams and bulk ingestion through
ADBC, plus a `redis-adbc` CLI for inspecting, copying, adopting and sweeping HASH
collections. Intended consumers inferred from the API and examples include data
engineers, Python/Arrow applications and tools with usable ADBC integration.
The [ecosystem comparison](../data-engineering-adbc-support.md) distinguishes
possible integration paths from integrations actually tested.

The driver needs Redis Search/Query Engine and supports RAM configurations on
Redis Software, OSS standalone/cluster and documented Cloud endpoints. It
rejects Flex; [Flex probes](../redis-flex-compatibility.md) are experiments, not
a support declaration. Autocommit is the only transaction mode. The validation
quirks mark UNIQUE/FOREIGN KEY metadata and several other capabilities absent;
accepted SQL syntax must not be confused with enforced constraints.

## Source map

| Concern | Primary sources |
|---|---|
| Public usage, supported SQL, options and limits | [README](../../README.md) |
| Toolchain and dependencies | [go.mod](../../go/go.mod), [Makefile](../../go/Makefile) |
| ADBC C interface and handles | [pkg/driver.go](../../go/pkg/driver.go), [driver.go](../../go/driver.go), [database.go](../../go/database.go), [connection.go](../../go/connection.go), [statement.go](../../go/statement.go) |
| SQL execution and values | [sql.go](../../go/sql.go), [query.go](../../go/query.go), [eval.go](../../go/eval.go), [types.go](../../go/types.go), [sqlstate.go](../../go/sqlstate.go) |
| Storage, recovery and external collections | [store.go](../../go/store.go), [rekey.go](../../go/rekey.go), [adopt.go](../../go/adopt.go), [CLI](../../go/cmd/redis-adbc/main.go) |
| Arrow ingestion and streaming | [ingest.go](../../go/ingest.go), [arrowconv.go](../../go/arrowconv.go), [stream.go](../../go/stream.go) |
| Aggregate execution | [aggregate.go](../../go/aggregate.go), [parallel_aggregate.go](../../go/parallel_aggregate.go), [aggregate_spill.go](../../go/aggregate_spill.go), [exact aggregates](../partitioned-exact-aggregates.md) |
| Shared workers and learning | [worker_budget.go](../../go/worker_budget.go), [worker_tuner.go](../../go/worker_tuner.go), [autotuning design](../worker-autotuning.md) |
| Capabilities and validation | [Redis quirks](../../go/validation/tests/redis.py), [pgdiff guide](../../go/pgdiff/README.md), [test workflow](../../.github/workflows/test.yml) |
| Packaging and diagnostics | [release workflow](../../.github/workflows/release.yml), [diagnostics guide](../execution-diagnostics.md) |

## Architecture facts that constrain changes

- Full row values live in HASHes. A hidden `__rowid` preserves rows containing
  only NULLs; absent fields normally encode NULL, with documented default-value
  exceptions. Search indexes accelerate filters/sorts/aggregates.
- Strings in TAG indexes can normalize or truncate values. Exactness metadata
  and residual evaluation decide whether pushdown is safe. Numeric precision
  also constrains reductions. Correct fallback is part of the contract.
- Metadata keys share the `{meta}` cluster hash tag; row keys distribute across
  slots. Escaped identifiers and never-reused physical prefixes prevent namespace
  collisions and stale writers from contaminating replacement tables.
- Streaming separates execute from fetch. Cancellation/close must reclaim
  cursors and release Arrow resources and aggregate worker grants.
- Worker allocation is shared per database handle. Automatic learning uses
  observed work and runtime capacity, without replaying SQL or changing the
  application's `GOMAXPROCS`. Explicit worker and budget options constrain it.

## Historical data, with limits

| Evidence | Recorded result | Interpretation |
|---|---|---|
| [README validation](../../README.md#compatibility-and-validation), 2026-10-08 | Software: 360 passed, 8 skipped, 3 subtests passed, 0 failed | Historical run; Cloud/TLS results in that section are older |
| [pgdiff baseline](../../go/pgdiff/README.md#measured-baseline), 2026-10-09, `f0cf7e9` | 327/603 scored (54.2%); 607 total; 13/13 smoke and 19/23 captured dbt cases | Curated corpus coverage, not full PostgreSQL conformance or a fresh HEAD result |
| [Worker scaling](../../benchmarks/workers/2026-10-08/README.md), 100,000 rows, `5904851` | README reports mixed ungrouped aggregate 729.9 ms at one worker vs 330.3 ms at auto (10) | Fixed-QPF, single-query historical comparison before measured autotuning; client CPU/RSS increased |
| [Controlled autotuning gate](../../benchmarks/worker-autotune/controlled/evaluation-2026-10-09/gate.json) | `cancelled`; 47/222 invocations and 141/666 records retained | No complete three-repetition comparison; readiness unproven |

The stopped run's [problems](../../benchmarks/worker-autotune/controlled/evaluation-2026-10-09/PROBLEMS.md)
record provisional cases outside intended bounds and the frozen measured source
`09588f0ca874a34cfbf092cf39ecdf06cbed6a0c`. Subsequent fixes were tested separately,
not measured by that matrix. Preserve that distinction in future claims.

## Unknowns

The checkout does not establish named product/security/release owners, current
Cloud compatibility, production SLOs, branch rules, agent-eval pass rates or
production incidents. Recent commits show catalog/session/encoding and
integration-documentation work; commit titles alone do not establish release or
deployment status. Resolve these facts from their owners or live systems before
depending on them.
