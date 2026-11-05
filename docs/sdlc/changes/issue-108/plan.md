# Plan and evidence: issue 108

Date: 2026-11-04. Source: `f1e1f03`. Author: Codex.
Status: implementation and verification in progress.
See [intent](intent.md), [spec](spec.md), [issue](https://github.com/redis/redis-adbc-driver/issues/108).

1. Reproduce unsupported GRANT/REVOKE parsing. Completed: focused tests failed
   with `unsupported statement starting with "grant"` / `"revoke"` before changes.
2. Implement restricted SELECT parser, database management option, dedicated
   ACL API adapter, durable lock/ledger and grant catalog views.
3. Verify real Redis Software enforcement and failure boundaries; review diff
   against REVIEW.md correctness, access ownership and evidence passes.
4. Document setup/recovery and exact compatibility limits; submit PR, inspect
   checks, merge when eligible, and verify issue closure.

## Verification evidence

- Before implementation, the retained parser cases reproduced unsupported GRANT
  and REVOKE statements. The requestor then chose enforced SELECT mapping rather
  than no-ops, so unsupported privilege forms intentionally remain errors.
- `env -u REDIS_URI -u REDIS_RBAC_TEST_API go test -count=1 -p 1 -json ./...`:
  exit 0, 946 passing test/subtest events after rebasing onto `d2c94f5`, 604 environment-dependent skips;
  all six Go packages passed. Redis/GPU/profile-dependent skips do not prove
  integration compatibility.
- `make -C go build`: exit 0, macOS arm64 shared library built.
- Focused parser, ACL ownership, shared-binding rejection, mixed-role rejection,
  TLS verification, credential redaction and neighboring session/catalog checks:
  exit 0 (27 focused test/subtest passes in the session/catalog run).
- `REDIS_RBAC_TEST_API=https://localhost:29648 go test -count=1 -p 1
  -run 'TestPrivilege|TestRBAC|TestSQLRBACSelect' -v .`: exit 0. Verified complete
  reads after grant, denied unrelated table/writes, idempotence, rename,
  revocation, stale-prefix revoke/re-grant after TRUNCATE, catalog discovery,
  lock contention, and lock retention after an accepted write with a failed
  response. Mixed-role bindings fail before either ACL or memberships change.
- Dedicated local container: `redislabs/redis:8.2.0-78.18`, Redis 8.6.2,
  Search, one shard, TLS, 4x QPF configured by software-setup.sh. Grant fixtures
  ran sequentially because database binding updates are shared state.
- Companion adapter: redis/redis-dbt-adapter source `0757da9` plus this change,
  dbt Core 1.12.6 / Python 3.13. Unit tests: 14 passed. Its retained opt-in
  integration test passed: dbt incremental model grants SELECT, an unchanged
  rerun recognizes current grants, and an empty SELECT list revokes; a separate
  reader fetched `(42,)` after grant and received NOPERM after revoke.
- Combined-role exploration denied reads before SQL ACL changes in the tested
  Software fixture. Initial support therefore explicitly rejects any bound
  role used by users with multiple memberships; this is retained as a failing-
  closed regression, not advertised as supported multi-role authorization.

Self-review followed REVIEW.md's correctness, ownership and evidence passes.
No human review approval is inferred. Scope limits: Software management API only,
one base-table SELECT privilege/role per SQL statement, single-role readers,
operator-provisioned bindings and verified API/database association, asynchronous
propagation, prefix-change re-grants, and explicit cross-system recovery. Use
non-evicting control storage and reconcile after metadata loss. The live RBAC test is now an explicit Software CI step after sequential Go
tests. Driver PR: [#310](https://github.com/redis/redis-adbc-driver/pull/310);
companion dbt PR: [#41](https://github.com/redis/redis-dbt-adapter/pull/41).
CI is pending.
