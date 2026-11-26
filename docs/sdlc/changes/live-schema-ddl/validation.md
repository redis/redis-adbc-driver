# Validation

Date: 2026-11-26. Source base: `49827869` plus this uncommitted change (named live
schemas, live table DDL, and the follow-up live `CREATE TABLE`).
Author: Claude. Requestor: Albert Wong. Status: verified locally; human review pending.

## Environment

A dedicated local Redis Software fixture, created with
`COMPOSE_PROJECT_NAME=adbc-live-schema SOFTWARE_API_PORT=19843
SOFTWARE_DB_PORT=19879 bash go/software-setup.sh` (image `redislabs/redis:latest`,
Redis 8.8.0, three shards, RAM/noeviction, TLS, ACL user). Every test command ran
through `software-setup.sh --exec`, which re-verifies the management API's
noeviction policy and active 4x QPF before running and exports the retained-control
policy. macOS client over the Docker published localhost port. Other sessions'
fixtures were not touched; other workloads shared the host.

The macOS sandbox blocks `*.key`/`*.pem`, including the fixture's own generated
test TLS key and CA, so fixture setup and test commands ran outside the sandbox.
Package downloads were unavailable in this environment, so the Python check used
an existing local `go/validation/.venv` (Python 3.14.10, read-only) instead of a
fresh environment.

## Checks and results

Snapshot-harness suites use the same disposable endpoint as `REDIS_SNAPSHOT_URI`.

- **New integration tests** (`-run '^TestLiveSchema'`, both URIs): all nine pass.
  They cover the schema DDL lifecycle, DROP TABLE preflight and multi-table removal,
  managed/live name collisions (hidden schemas without `live_tables`, an adoption
  blocker, managed creation refused), the DROP SCHEMA tombstone and stale-reservation
  withdrawal, Arrow create/append and a Dagster-style staged swap, `live_hash`
  partitions, evolution in a renamed named schema, and SQL `CREATE TABLE`. The
  `CREATE TABLE` test covers default and explicit prefixes, NOINDEX, comments,
  constraint and option refusals, occupied and index-covered prefixes (including
  a renamed table's), CTAS from live tables, CTAS refusal of managed sources and
  all-NULL rows with nothing created, Arrow append into a SQL-created table, DROP
  keeping HASHes, and `redis_live`.
- **REDIS_URI live suites** (`-run '^(TestLive|TestSQLMerge|TestSQLReturningMerge|
  TestStream|TestScanProjection|TestUnmanagedProjection|TestDropRename|TestCatalog)'`,
  before the `CREATE TABLE` follow-up): exit 0, 78 passed, 15 expected skips
  (snapshot-harness tests, a subprocess helper, opt-in profiles).
  `TestLiveHashesSQLAndRedis` now asserts `DROP TABLE redis_live.…` unregisters and
  keeps HASHes, TTLs, unknown fields, binary and nested keys.
- **Full Software suite** (`go test -count=1 -v -p 1 -timeout=30m ./...`, REDIS_URI
  only, before the `CREATE TABLE` follow-up): every package passed except one root
  test. The root package had 1,587 passed, 450 skipped and 1 failed:
  `TestSQLRekeyConcurrentWriters` ("timed out waiting for intervening source change
  or verified writer admission").
- **Full snapshot suite** (root package, `REDIS_SNAPSHOT_URI` only, including the
  `CREATE TABLE` code): 1,281 passed, 738 skipped, 16 failed. Twelve failures were
  environmental: those tests also read `REDIS_URI`, which this invocation unset on
  purpose ("invalid URL scheme"). All twelve pass when rerun with both URIs. Three
  were this change's own tests: two still asserted that `CREATE TABLE` in a live
  schema is refused, and one used a single `SCAN` call, which is not guaranteed to
  return a match on three shards. All three are fixed and pass. One,
  `TestManagedCandidatePagesBoundedAndComplete` ("redundant candidate queries: 4,
  maximum 3"), **fails identically on a pristine `git archive` of `49827869`**
  against the same fixture. It is pre-existing.
- **`TestSQLRekeyConcurrentWriters`** failed twice on this branch, both times under
  heavy host load (the full suite with concurrent builds, then right after a long
  suite). It then passed 7 of 7 branch runs, including replays of the sequences
  that preceded the failures (with and without the live schema tests first). It
  passed 5 of 5 runs on the base commit. Its path (managed table, `live_tables`
  off) reaches none of the new live branches. It is recorded as intermittent and
  not reproduced, not as proven unrelated.
- **Python DBAPI check** (scratchpad script, built `libadbc_driver_redis.dylib`,
  `adbc_driver_redis.dbapi`): passed. It covers `CREATE SCHEMA IF NOT EXISTS`
  twice, Arrow `create` with `live_source` and `append` into a named schema, SQL
  `CREATE TABLE` with generated `<schema>:<table>:` keys, CTAS with
  `WITH (prefix = …)`, the NOT NULL refusal, a Dagster-style
  `DELETE`/`DROP TABLE IF EXISTS`/`ALTER TABLE … RENAME TO` swap, `ALTER SCHEMA …
  RENAME TO`, `adbc_get_objects`, and `DROP SCHEMA … CASCADE` keeping HASHes.
- **Unit-only pass** (`env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1
  ./...`): every package ok.
- `go generate .` and `go run ./internal/cmd/genfunctions -check`: passed. One new
  body duplicated an existing inline body; it now reuses a single constant.
- `make -C go build adbc-cli`: exit 0 with repository version flags.
- `gofmt -l` (only pre-existing `infoschema_test.go`), `go vet ./...` (one
  pre-existing copylocks finding in `live_ingest_creation_integration_test.go`),
  `git diff --check`, and the local Markdown link/anchor check: passed.

- **Final root Software suite on the finished code** (`go test -count=1 -v -p 1
  -timeout=30m .`, REDIS_URI only, including the `CREATE TABLE` follow-up): exit 0,
  1,583 passed, 452 skipped, 0 failed. `TestSQLRekeyConcurrentWriters` passed in
  this run.

## Review and limits

Not exercised: Cluster API, restricted ACL principals, failover, Flex (live tables
remain rejected there), and concurrent DDL races beyond the deterministic
tombstone/withdrawal fixtures. That includes two concurrent `CREATE TABLE`s on
the same new prefix, which, as with Arrow creation, can both pass the
empty/overlap checks. The lost-reply orphan case in the spec is also not
exercised. No human review yet.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
