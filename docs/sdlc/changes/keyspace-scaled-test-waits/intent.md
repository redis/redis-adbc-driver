# Intent

2026-11-25; base `268ccf03` (main). Requested by the TPC-H issue work for
[#653](https://github.com/redis/redis-adbc-driver/issues/653): integration tests failed on
databases that also held large unrelated data (TPC-H fixtures), and change records set the
failures aside as fixture conditions. Status: local test-only change, not released. See
[spec](spec.md), [plan](plan.md), [validation](validation.md).

Three test helpers assumed a small keyspace or an uninterrupted earlier run:

- Retained-store cleanup (`retainedFixture`) ran `SCAN <prefix>* COUNT 100` under a fixed
  10-second context. SCAN visits every key in the database, not only the test's.
- `hashFixture.appIndex` waited a fixed 10 seconds for `FT.INFO` to report an application
  index complete, and `TestLiveHashPartitionIndexDefinitionChanged` restored its live index
  with a fixed 5-second wait. A new index's initial scan also visits every key.
- Live-table tests register fixed names such as `it_live_partition550`. A run that is
  interrupted (a `go test` timeout skips `t.Cleanup`) leaves the registration behind, and
  every later run fails with `live table already registered or capacity reached`.

Make those tests pass whatever unrelated keys the database holds, and recover from a stale
registration of the fixture's own prefix. No driver code changes, no assertion is weakened and
no skip is added. Out of scope: databases already at `maxmemory` before a test starts (the
server's OOM error already names that precondition), other sessions using the same database
concurrently (suites need a dedicated database), and production readiness waits, which are
bounded by the statement context rather than a fixed deadline.
