# Shared snapshot metadata discovery: validation

Date: 2026-11-10. Source baseline: `2c50655c`; final source is the PR head containing
this record. Issue: [#365](https://github.com/redis/redis-adbc-driver/issues/365).
Related: [intent](intent.md), [specification](spec.md), [plan](plan.md).

## Environment and provenance

Disposable authenticated TLS Redis Software 8.8.0 with Search, noeviction and
three primary shards. `make -C go redis-up` and the `software-setup.sh --exec`
wrapper verify active 4x QPF, effective workers/routing and management policy.
Operational identifiers, credentials, certificates and local build artifacts
remain private/ignored. No release containing the capability is claimed.

The working branch initially developed a separate implementation while PR #369
was being merged. That implementation was replaced with current main before this
metadata fix; its earlier test results are not evidence for the final runtime.

## Checks

- Before the fix, `TestSharedSnapshotUnavailableObjects` failed under both lease
  loss and expiry: catalog-only discovery succeeded, as did all tested depths
  with a nonmatching catalog filter. This ran against the merged #369 runtime.
- After the fix, the same regression passed with `-race`, alongside shared
  metadata/sequential reads, streamed ownership, failure/cancellation, unsupported
  use, quotas, compaction, admission mutex and pin cleanup regressions. The
  verified wrapper ran `go test -race -count=1 -p 1 -timeout=15m -v -run
  '^Test(SharedSnapshot(UnavailableObjects|MetadataAndSequentialReads|StreamOwnership|FailureAndCancellation|RejectsUnsupportedUse|Quota|SurvivesCompactionAndOptionContext|AdmissionDoesNotHoldBindingMutex)|SnapshotPin)' .`:
  passed in 9.641 seconds, with no race reports.
- `cd go && env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`:
  all packages passed. Endpoint-dependent integration tests skip in this
  unit-only pass; it alone is not Redis Software evidence.
- Verified Software wrapper with management-backed RBAC enabled,
  `go test -count=1 -p 1 -timeout=15m -run '^TestSQLRBACSelect$' .`: passed in
  22.572 seconds, including the merged shared-view revocation test.
- `make -C go build`: passed with the Makefile version linker flag. Its `v0.1.0`
  development version is not a published-release claim.
- Verified wrapper, final native library, package/library paths configured,
  `python -m pytest -q python/tests/test_shared_snapshot.py`: one test passed
  in 1.11 seconds. Python 3.15, ADBC manager 1.13.0, PyArrow 26.0.1.
- `python scripts/prepare_docs.py`, `mkdocs build --strict`, and
  `python scripts/check_docs.py`: passed 42 public pages, local links/assets,
  canonicals, sitemap, search and publication-scope checks.
- `gofmt` on the changed Go files and `git diff --check`: passed.

## Review and limits

Self-review followed [REVIEW.md](../../../../REVIEW.md). Correctness/lifecycle:
one operation reference owns all enumeration callbacks and final validation;
constructed readers are released on failure, and successful materialized readers
retain no server pin. Security/ownership: no ACL, grant, storage protocol or
parent/child lifecycle semantics change; the existing authenticated client still
executes all metadata commands. Intent/evidence: unavailable-view bypass is
reproduced and retained as a regression, API documentation matches #369, and no
release or Flight SQL support is claimed. No remaining actionable finding was
identified in these local passes; this is agent self-review, not maintainer
approval.

The full Software suite, upstream Python validation corpus, process-death suite
and transport-specific deployment matrix were not rerun for this metadata fix.
Original process/retention evidence remains in the
[merged feature record](../shared-read-snapshot/verification.md). Issue #365 stays
open for an actual released version and connector integration/enablement evidence.
