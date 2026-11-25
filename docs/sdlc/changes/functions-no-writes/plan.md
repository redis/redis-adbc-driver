# Read-only Redis Functions plan and validation

2026-11-25 · PR base: 16032de1 (live runs on the same change atop 9ea7ba76; the four intervening commits only spill joins and sorts) · Status: implemented and locally validated

See [intent](intent.md) and [specification](spec.md); implemented in [redis/redis-adbc-driver#683](https://github.com/redis/redis-adbc-driver/pull/683).

1. Inventory every Function body and call site. Audit each body for write and
   may-replicate commands, including commands that shared fragments define but
   don't reach (the directory notification's `HSET` is reached: it stays a writer).
2. Add `newReadOnlyRedisFunction`, the generated read-only list, the flagged
   registration form and flag-aware versioning. Select `FCALL_RO` from the
   manifest in `Args`/`Run`/`runScript`, the retained helper and the Search
   admission gate. Scope `FCALL_RO` keys in the snapshot hook.
3. Extend `genfunctions` to record read-only declarations and refuse conflicts
   or raw `redisFunctionName` callers of read-only bodies.
4. Grant `+fcall_ro` in the generated SQL RBAC reader selector, the documented
   writer profile and the fixture principals. Replace `+fcall` with it for the
   read-only retained worker. Update the Functions, RBAC, retained-partition and
   snapshot docs, plus CLAUDE.md.
5. Add the unit audit and the owned-database over-maxmemory integration test.
   Capture the failure before the fix; run the unit pass, ACL tests and the full
   suite.

## Evidence

The client was macOS arm64 over Docker-published localhost ports. These are
correctness checks, not performance claims. Endpoints, credentials and raw logs
stay private. The Redis Software fixture was an owned `make redis-up`-equivalent
(`software-setup.sh` with its own compose project and ports): Software image
`redislabs/redis:latest`, Redis 8.8.0, three RAM shards, 1 GiB, `noeviction`,
TLS, with active and effective 4x QPF (6 Search workers, `mnp`, 32 connections)
verified by the script and again by the over-maxmemory test.

- Full Software suite (`make test-software`, which re-verifies 4x QPF): passed.
  1,577 top-level passes, 443 skips, zero failures; root package 1,121.3 s. The
  over-maxmemory test skips there because it is opt-in; snapshot cases skip
  without `REDIS_SNAPSHOT_URI` (run separately below).
- Over maxmemory on Software (`REDIS_FUNCTION_OOM_TEST_API`): passed 3/3
  back-to-back runs, then `memory_size` was restored to 1 GiB. Software applies a
  new `memory_size` to its shards some time after the management API reports it,
  and sometimes not until it changes again. One measurement wrote 300 MiB under
  a reported 192 MiB limit without a refusal; others refused within about 55
  MiB. An earlier version of the test trusted the API and once filled shards to
  about 1 GiB each. The test now fills within a byte budget (twice the limit plus
  64 MiB) and retries with a 1 MiB lower size until the limit is enforced; two of
  the three passing runs needed one or two retries. The halved limit gets the
  same treatment.
- Software restricted-principal gates: `TestRedisFunctionsRejectEvalOnlySoftware`
  (principal `~* +@all -fcall -fcall_ro`, provisioned through the management API)
  passed under `-race`, with the `Redis Functions are required` refusal.
  `TestSQLRBACSelect` (`REDIS_RBAC_TEST_API`) passed all 13 subtests with the new
  `(+fcall +fcall_ro …)` reader ACL.
- Snapshot suite as CI selects it (tests named `Snapshot`, `-race`,
  `REDIS_SNAPSHOT_URI` set to the fixture, through `software-setup.sh --exec`):
  passed, 405 top-level passes, 68 skips, zero failures, no data races; root
  package 794.1 s. It covers the point read, Search admission, namespace read,
  capability probe and operation status callers.
- Full suite on owned standalone Redis Open Source 8.6.2 over TLS: passed, 1,594
  top-level passes, 426 skips, zero failures; root package 1,358.9 s.
- Failing case before the fix: `redis/main` 9ea7ba76 exported, a scratch test
  with the same fill-then-halve maxmemory procedure, owned standalone Redis Open
  Source 8.6.2 over TLS with `noeviction`. The unflagged namespace read returned
  `OOM command not allowed when used memory > 'maxmemory'` three times out of
  three, and the connection probe failed with `Redis Functions are required: OOM
  command not allowed …`.
- Same server, this change: `REDIS_FUNCTION_OOM_TEST_CONFIG=1 go test -run
  '^TestRedisReadOnlyFunctionsOverMaxmemory$'` passed 3/3. Main's unflagged
  library (48 functions, 0 `no-writes`) and this change's library (48 functions,
  9 `no-writes`) were installed side by side. Cleanup restored `maxmemory 0` and
  removed every test key.
- A first version of the test filled only until 1 MiB writes were refused. Used
  memory then drifted around the limit and small commands sometimes passed, so
  the test fills in decreasing sizes and then halves the limit under the data.
- Mutation checks: dropping the flags from the generated library fails
  `TestRedisFunctionReadOnlyFlags` ("library declares 0 flag tables, want 9").
- `env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages passed.
  `go run ./internal/cmd/genfunctions -check` passed on the final source. (Moving
  three inline probe bodies into named constants had left the manifest's source
  text stale. Regenerating kept the identical library name, so the live runs
  used this library.) `gofmt -l` reports only the
  pre-existing `infoschema_test.go`, and `go vet` reports only the pre-existing
  `live_ingest_creation_integration_test.go` lock-copy warning.
- OSS ACL tests (`go test -run ACL`; Software blocks `ACL SETUSER`): every test
  passed except the retained minimal-worker test before its fixture change. It
  failed with `NOPERM … 'fcall_ro'`. With `+ping +fcall_ro +time +getrange` (no
  `+fcall`) it passed with an empty ACL log for the worker.

- Python Software ACL scripts, after building the validation venv (`uv sync`,
  run outside the agent sandbox because PyPI is off its allowlist) on a fresh
  owned fixture prepared by `software_partition_tls.py` (verified API and
  TLS-only proxy, 4x QPF re-verified). `snapshot_control_script_software.py`
  passed `TestRedisFunctionsRejectEvalOnlySoftware` (operator
  `+@all -fcall -fcall_ro`) and `TestSnapshotControlScriptPointLeaseSoftware`.
  `snapshot_gc_acl.py` passed the GC, import, replace and insert permission
  preflights and cleanup recovery, 5/5, with the `+fcall_ro` grants. The
  offline `scripts/test_*partition_tls.py` passed 49/49.

Not run locally: `retained_partition_tls.py` runs only inside
`check_partition_tls.py`, which needs a built driver library and its own TLS
fixture. PR CI runs it, and it includes the retained worker grant. The new
integration test is opt-in, so CI skips it. This session did not test Flex.
The requesting session's dedicated Flex database refused unflagged `FCALL` at
its logical limit while `FCALL_RO` of the `no-writes` body answered (see
[intent](intent.md)). It never triggered the RAM-part `bigredis-max-ram`
refusal, which stays unverified.

## Review

Agent review against [REVIEW.md](../../../../REVIEW.md); no human approval is
implied. Lifecycle: no replay, fallback or retry was added; the probe still
installs only after a direct `ERR Function not found`. `FCALL_RO` is never
fenced and is scoped like `FCALL`. ACL/compatibility: every driver user now
needs `+fcall_ro`. Existing dedicated SQL RBAC ACLs need re-provisioning, and
old grant writers refuse the new baseline (documented in SQL RBAC). That is a
deliberate breaking upgrade step consistent with 94e123d6, not a migration.
