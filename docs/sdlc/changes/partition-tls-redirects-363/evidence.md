# Validation evidence

Date: 2026-10-21. Base: `70a0d43` (#417). Author: Codex. See [intent](intent.md),
[spec](spec.md), [plan](plan.md) and [source/hash metadata](evidence/validation.json).
No Go runtime code changed. Native runtime includes #416's Search parser;
its library SHA-256 is recorded in every final report and the installed wheel.

## Final live checks

| macOS ARM64 client | Routing | Result | All-primary ACL cleanup |
|---|---|---|---|
| Native library, Python 3.13.16 / ADBC manager 1.12.0 | explicit Cluster API | [19 passed](evidence/native-explicit-final.json) | [passed](evidence/native-explicit-final-cleanup.json) |
| Installed macOS 12+ ARM64 wheel, Python 3.14.8 / ADBC manager 1.8.0 | automatic Cluster API | [19 passed](evidence/macos-minimum-wheel.json) | [passed](evidence/macos-minimum-wheel-cleanup.json) |

**38 final TLS checks passed** against the same disposable Docker Redis
8.6.2/Search 8.6.0 three-primary fixture, with fresh schema/users per run.
Both runs retain RESP3 and certificate/hostname verification. Two fresh worker
processes warm persistent native connections before migrating a slot containing
only an owned row. Direct source probes confirm real ASK and MOVED responses.
ASKING is denied on every primary, independently checked as NOPERM; the native
read must fail while denied, then return exact values/schema after grant.
The second native connection remains idle until ownership is finalized and also
reads the original descriptor successfully. All three primaries must agree on
the new owner. Subsequent full Search reads validate all original values/schema;
direct node pipelines prove all 1,099 rows remain on their expected physical owners.
Later update/delete, high-watermark, stale-target and lifecycle checks still pass.

Search count probes needed 10 attempts for the final explicit run and one for
the installed-wheel run. This is a bounded fixture readiness barrier after
migration, not transparent runtime query/stream recovery. No native read/write
is retried by this harness. The denied native read is deliberately retried only
after the administrator changes its ASKING permission.

Management/fault checks: **30 passed**, including five new cases for changed
primary identity, unrelated/unbounded slot keys, refusing ownership reassignment
with remaining source keys, bounded Search convergence/unrelated error propagation,
and reaping an unresponsive child even with broken stdin.
[Output](evidence/fault-tests.log). Python compilation and whitespace checks passed.

## Reproduction and corrections

Use the existing [TLS fixture command](../../../partition-cluster-tls.md).
Automatic routing is the default; add `--cluster-mode true` for explicit routing.
The installed-wheel run unsets REDIS_ADBC_LIBRARY and adds `--require-wheel`.
No additional CI job or packaging trigger was added: existing Test native and
Release installed-wheel invocations automatically include the two new checks.

The first development assertion required literal NOPERM in the native error;
redirect handling returned an error without that marker. The final contract
checks NOPERM independently on each primary and a visible native failure followed
by exact success after grant. It makes no native error-string guarantee.
[Initial failed report](evidence/initial-error-marker-failure.json).

A later explicit-mode run passed routing and post-move reads but failed its
subsequent UPDATE with `Query requires unavailable slots`. The bounded all-primary
exact-count readiness probe was added to wait for Search topology convergence,
with deadline failure and unrelated errors remaining terminal.
[Initial failed report](evidence/initial-search-convergence-failure.json) and
[cleanup](evidence/initial-search-convergence-cleanup.json) preserve that result.
The first wheel build lacked the build module in the validation environment;
the final wheel was built and installed in a separate environment with the
required packaging dependencies.

## Cleanup, review and limits

Both final runs verified owned planner/worker users absent on every primary.
The exact identity/label-verified cluster container was removed, all private CA,
keys/passwords/configs were deleted with the fixture directory, and the isolated
client environment was removed. No paid or Cloud resources were created.

Agent review followed REVIEW.md for correctness/lifecycle, TLS/ACL ownership,
error handling, exact data/schema checks, and scope of evidence. No actionable
findings remain. This is not human approval; PR review/hosted checks are pending.

Linux local clients, Cloud/Software changes, full Go/upstream Python suites,
replica failover, concurrent writes during resharding and mid-stream recovery
were not exercised here. The baseline PR #417's hosted Test run 39690236282 had
passed both Linux OSS-cluster jobs when inspected; that does not validate these
new migration checks. Issue #363 stays open for broader compatibility and
performance evidence. This fixed-data manual slot migration supplies no snapshot,
replay, exactly-once, automatic failover or production performance guarantee.
