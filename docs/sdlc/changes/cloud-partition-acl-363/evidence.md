# Cloud Pro independent-credential evidence

Date: 2026-10-21. Source: 1bb46ca (merged #405), with this change's Python
management/checker additions. Requestor and resource owner: Albert Wong.
Agent: Codex. Human review pending. See [intent](intent.md), [spec](spec.md),
[plan](plan.md), [runner instructions](../../../cloud-partition-validation.md),
and [deployment record](evidence/deployment.json).

## Deployment and credentials

Dedicated Redis Cloud Pro RAM database, AWS us-west-2, one primary shard through
the proxy; Redis 8.6.2 and Search 8.6.11. Requested 0.1 GB and 1,000 operations
per second; no replication/persistence, no eviction, verified TLS. The dry run
listed MinimumPrice $0.274/hour and one Shards item at $0.043/hour. These are
estimate components, not an invoice or measured total. API keys stayed in parent
process memory and were never written into fixtures, workers or git.

Each planner/worker has a fresh account ACL user, rule and role; each role maps
only to this subscription/database pair. The worker has read-only key patterns
and a command allowlist. Fresh subprocess workers receive only their own URI,
CA path, native library and descriptor in stdin. Administrative/planning URIs
and API credentials are excluded from their environment.

The trust bundle contains normal TLS roots plus a generated local CA for a
correctly signed wrong-host test server. It is an explicit test bundle; OS trust
was not changed. Cloud's actual aggregate cap was 2,147,483,648; planning used a
verified lower bound of 137. Cloud filters unauthorized CONFIG GET to an empty
reply and denies CONFIG SET and HSET, matching the managed-proxy assertions.

## Setup corrections retained

The first user creation rejected an all-hex password with
`ACL_USER_PASSWORD_NOT_VALID`. Cloud passwords now contain upper/lowercase,
digits and punctuation within 32 characters. A later permission update rejected
an unchanged name with `ACL_REDIS_RULE_NAME_ALREADY_EXISTS`; omitting the name
from that permission-only PUT succeeded in a focused unassigned-rule probe.
The probe and partial-run principals were removed and absence verified. No test
assertions or skips were relaxed. See [setup corrections](evidence/setup-corrections.json),
[password rejection](evidence/initial-password-rejection.json),
[update rejection](evidence/initial-rule-update-rejection.json), and
[partial-run cleanup](evidence/initial-rule-update-cleanup.json).

## Checks run

- Current client: macOS arm64, Python 3.13.16, ADBC manager 1.12.0, Arrow 26.0.0,
  native library selected explicitly. **16 checks passed**; both independent
  principals authenticated; all six ACL resources were deleted and verified
  absent. [Result](evidence/cloud-current-client.json),
  [cleanup](evidence/cloud-current-client-cleanup.json).
- Minimum client: macOS arm64, Python 3.12.15, ADBC manager 1.8.0, Arrow
  26.0.0, installed wheel with `--require-wheel` and no native override.
  **16 checks passed**. [Result](evidence/cloud-minimum-wheel.json),
  [ID-based cleanup](evidence/cloud-minimum-wheel-cleanup.json).
- Existing OSS native TLS regression: **15 checks passed**, preserving the
  pre-existing cap and permission assertions. The owned local process was stopped.
  [Result](evidence/oss-regression.json).
- Management fault tests: **13 passed** (10 Cloud, 3 existing Software), covering
  ownership rejection, exact database role scope, no POST replay/secret-body
  logging, management TLS trust isolation, permission-only PUT, partial failures, uncertain creation, atomic
  journal interruption/recovery, ID-based cleanup and continued cleanup after
  failure. They are now in the regular Test workflow on both OSS-single arches.
- Python compilation, workflow actionlint and diff whitespace checks passed.

The Cloud checks verify exact Arrow values/schema after planner closure, Search
and direct HASH reads, duplicate/deleted IDs, native bad-CA and wrong-host
rejection, CONFIG/write denial, malformed descriptors, FT.INFO and HMGET denials,
cancellation, early release, live mutations/later insert exclusion and stale
physical targets. Result files list every check; none is skipped.

The previous full configured Python Cloud suite (371 tests plus 3 subtests
passed, 8 feature skips) remains in [#405 evidence](../cloud-pro-validation-363/evidence.md).
It was not rerun here: Go runtime source and ordinary-query behavior did not
change. This change focuses on independent identities and management lifecycle.

The final management trust-context refinement also passed a live cleanup replay:
all six recorded IDs remained absent. [Replay](evidence/final-management-cleanup-replay.json),
[context check](evidence/management-context-check.json). Deployment metadata records
minimum-wheel-run and final management harness hashes separately; final error/lifecycle refinements
are covered by the fault tests and the minimum-wheel run.

## Deployment cleanup and review

The owned Pro database and subscription were deleted. Both resource GETs returned
HTTP 404; subscription absence was verified at 2026-10-21 21:32:41 UTC. Temporary
Cloud/OSS fixture credentials and private keys were removed. The local OSS
process was stopped. No shared databases or existing ACL resources were modified.
[Cleanup record](evidence/cleanup.json).

Agent review covered ownership, asynchronous/uncertain outcomes, dependent cleanup,
TLS context isolation, descriptor and worker credential boundaries, regression
assertions and evidence scope. No actionable findings remain in the final diff.
Human review and new-PR hosted checks remain pending. Cloud sharding and broad
client/platform/performance claims remain excluded.

## Remaining scope

Single primary shard only. Cloud proxy sharding, Cloud OSS Cluster API, the Linux
Cloud client matrix and remaining Python/ADBC versions still need evidence. No
Cloud performance measurements were made. Issue #363 remains open.
