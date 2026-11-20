# Live MERGE validation

Date: 2026-11-20. Base: `27e5a8d2`. Requestor: Albert Wong. Author: Codex.
Issue: [#553](https://github.com/redis/redis-adbc-driver/issues/553).
Status: focused validation passed; human review pending.

Provisioned a dedicated local Docker Redis Software fixture with `make -C go
redis-up`. The default Docker network pool was exhausted; an external private
Compose override selected an unused subnet without modifying other fixtures.
The fixture, endpoint, TLS material and logs remain private. Reused the fixture
through `software-setup.sh --exec`, which verifies Redis Software 8.8.0, three
active primary shards, RAM/noeviction, TLS, active QPF factor 4, six Search
workers, mnp routing and 32 connections before launching tests.

From `go/`, with fixture-specific Compose/port environment configured:

```sh
bash software-setup.sh --exec go test -count=1 -v -p 1 -timeout=10m \
  -run '^(TestLiveMerge.*|TestLiveHashesSQLAndRedis|TestLiveGeneratedInsertKeys|TestLiveExactValuesAndSchema|TestLiveUpdateMissingRowRefusesResurrection|TestSQLMerge|TestSQLMergeDbt|TestSQLReturningMerge)$' .
```

Exit 0: 13 top-level tests and seven subtests passed; no skips/failures.
New coverage verifies matched UPDATE/DELETE, unmatched INSERT, NOT MATCHED BY
SOURCE, DO NOTHING, action-specific RETURNING and affected-row counts,
generated UUID keys, NULL values, TTL and unknown-field preservation,
duplicate-target preflight, explicit NULL/colliding keys, mixed managed/live
and modifying-CTE exclusions, live source joins, actual zero-byte keys in ON
and qualified RETURNING, hidden star columns, immutable keys and last-field
refusal. Injected lost/composed UPDATE replies, lost DELETE replies,
cancellation, vanished rows, permission refusal, registration-version change
and composed preflight read loss verify early termination and unchanged or
partially applied source state as appropriate. These injected permission errors
are not a real restricted-principal deployment test.

Existing ordinary live writes, generated keys, exact values, missing-row guards
and managed SQL/RETURNING MERGE regressions also passed. Earlier development
runs exposed unsupported VALUES alias-list fixture syntax, then joined live
`__key` hiding; the fixture uses supported SELECT/UNION ALL syntax and the
implementation now supports qualified logical live keys without exposing
internal row IDs. Subsequent focused runs passed.

Formatted changed Go files and ran `git diff --check`. No full suite, native C
boundary, independent-process, failover, race, Flex or performance run was
performed for this change. No speedup or full source snapshot claim is made.

Self-review covered correctness/lifecycle, source ownership, non-replay,
identity checks and documented scope. Live UPDATE uses the existing atomic
source script via one non-replaying command per row; unmanaged and managed
mutation paths retain their prior routing. Managed import provenance handling
is unchanged. No actionable findings remain from this self-review; this is not
human approval.

See [intent](intent.md), [spec](spec.md) and [plan](plan.md).
