# Plan and validation

Date: 2026-11-04. Base: `efb9cbf`. [Intent](intent.md), [spec](spec.md).

1. Update image pull policy and database creation defaults.
2. Preserve current TLS/authentication/three-shard and QPF safeguards.
3. Correct the QPF request to apply the required routing and connection count;
   retain the strict effective-configuration gate and add regression coverage.
4. Update compatibility documentation and attach the dated Flex probe replies.
5. Publish a PR linked to issue #314; do not close the unsupported-Flex issue.

Checks:

- Shell syntax, Compose configuration, and `git diff --check` pass.
- Software helper tests: 14 passed, including the new routing-default case.
  The regression case failed before the QPF request was corrected.
- Focused current-main SQL test passes on Software 8.2.6-25 / Redis 8.8.0 /
  Search 8.8.8 with three shards, TLS/authentication and verified 4x QPF.
- Historical full-suite and Flex results, including failures, are recorded in
  [compatibility](../../../compatibility.md) and the
  [Flex report](../../../redis-flex-compatibility.md). The full current-main
  suite and ADBC validation have not been rerun for this PR.

Review: inspected defaults, request payload, unchanged verification gate,
TLS/authentication/topology preservation, raw evidence, and historical claims.
No unresolved finding in this diff; no human review or merge approval implied.

## CI follow-up: managed CONFIG GET denial

Both Software-cluster jobs and both Linux release jobs failed at the same
CONFIG GET assertion: Redis 8.8 returned NOPERM where older proxies returned
an empty reply. The shared smoke check now recognizes these two blocked
responses without allowing readable configuration or swallowing other errors.

All 42 partition TLS management/helper tests pass, including regression
coverage for empty replies, explicit denial, readable configuration, and
unrelated server/connection failures. The live Software 8.8 TLS/RBAC rerun
passed all 18 native partition checks and five metadata checks on the owned
three-shard, verified-4x-QPF fixture, recording `software_proxy_config_get_denied`
and successful rejection of configuration mutations and data writes.

## CI follow-up: RBAC propagation assertion

The next amd64 Software job passed the corrected TLS/RBAC path. The arm64
job exposed a separate propagation race in `TestSQLRBACSelect`: after the
retry loop fetched a successful query, the assertion issued another query
outside that loop, which could still reach a shard applying the ACL.
Validate the same fully fetched result that passed the wait instead. Wrong
values still fail immediately, and the existing 20-second deadline remains.
This changes the integration test only, not grant/revoke behavior.

Validation: the focused RBAC test passed three consecutive runs on a fresh
Software 8.2.6-25 / Redis 8.8.0 three-shard TLS fixture with verified 4x QPF.
