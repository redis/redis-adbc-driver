# Shared read snapshots: validation

Date: 2026-11-10. Source baseline: `85509485`; implementation revision is the PR
commit containing this record. Status: completed local checks; release/integration evidence remains outstanding.
Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [specification](spec.md), [plan](plan.md).

## Environment

Disposable Redis Software 8.8.0 with Search, three primary shards behind one
Software proxy, authenticated TLS and noeviction. `make -C go redis-up` and
`software-setup.sh --exec` verify active 4x QPF, effective Search workers/routing,
primary shard readiness and the management policy before testing. Operational
configuration, certificates and management credentials remain in ignored local
build artifacts; no operational identifiers or secrets are committed.

## Checks

All results below are from the working tree represented by this PR, on macOS
arm64. The native library uses the Makefile's `v0.1.0` development version linker
flag; this is **not** a claim that a release containing the feature exists.

| Check | Result |
|---|---|
| `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...` | Passed all packages. Integration tests skip without their endpoints; this pass alone is not Software evidence. |
| Verified Software wrapper, `REDIS_SNAPSHOT_URI` set to its disposable endpoint and `REDIS_URI` unset; `go test -count=1 -p 1 -timeout=15m -v -run '^TestSharedReadSnapshot' .` | Passed the initial five shared-view tests, including actual process kill and eventual reclamation; 127.531 seconds. Process-death cleanup alone took 121.47 seconds. |
| Same verified wrapper; `go test -race -count=1 -p 1 -timeout=15m -v -run '^Test(SharedReadSnapshot(Catalog\|StreamOwnership\|Failures\|QuotaAndClose\|Substrait\|MissingData)\|SnapshotSubstrait\|SnapshotPin)' .` | Passed all selected tests and subtests, including the final strict child deadline, shared Substrait and missing-row checks; 19.151 seconds. No race reports. |
| Verified wrapper with management-backed RBAC enabled; `go test -count=1 -p 1 -timeout=15m -v -run '^TestSQLRBACSelect$' .` | Passed all subtests, including shared historical-view revocation/restoration; 27.025 seconds. |
| `make -C go build` | Passed with the repository version linker flag. |
| Verified wrapper, final native library, Python 3.15, ADBC manager 1.13.0 and PyArrow 26.0.1; `python -m pytest -q python/tests/test_shared_snapshot.py` (repository-relative path; run with package/library paths configured) | Passed the native two-table full-refresh test. |
| `python scripts/prepare_docs.py`, `mkdocs build --strict`, `python scripts/check_docs.py` | Passed: 42 public pages, local links/assets, canonicals, sitemap, search and publication scope. |
| `gofmt` on changed Go files; `git diff --check` | Passed. |

The first live run exposed an expiry diagnostic returning only a generic timeout;
subsequent checks passed after naming the failed shared view and retaining its
failed binding. The Python example uses the inspected DBAPI schema helper and
is exercised by the native regression.

## Review

Self-review followed the three passes in [REVIEW.md](../../../../REVIEW.md).
Correctness/lifecycle: immutable root reuse includes all GetObjects callbacks;
children have fresh validation state, explicit fixed deadlines and local cleanup;
parent release invalidates readers without rebinding them. Security/ownership:
read-only admission covers nested SQL writes, Arrow/retained operations, sequence
functions, refresh and partition APIs; no token is exported; native ACL revocation
remains effective. Intent/evidence: existing protocol 17 is retained, operational
artifacts are ignored, and public docs reject Flight SQL/shared attachment.
No outstanding actionable finding was identified in these local review passes.
This is agent self-review, not maintainer approval.

## Limits and remaining acceptance

The entire Software suite and upstream Python validation corpus were not rerun;
only the change-specific suites above were run with Redis. No native Cluster API
or Flight SQL shared-snapshot support is claimed by these checks. The real
process-death test ran without the race detector; the selected lifecycle tests
ran with it. There is no released version containing this PR and no end-to-end
Airbyte connector rollout validation. Keep #365 open for those explicit release
and integration acceptance items. This PR does not publish a release or merge
itself.
