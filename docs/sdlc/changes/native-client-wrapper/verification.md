# Native client wrapper verification

Date: 2026-11-11. Author: Codex. Status: initial implementation verified locally;
awaiting human review.
Scope: [intent](intent.md), [spec](spec.md), [plan](plan.md).

Shared library builds and integration checks passed against Redis Software 8.8.0
using the disposable three-shard test fixture. `make -C go redis-up` and
`software-setup.sh --exec` verified effective 4x QPF and noeviction before test
execution. Operational details remain private.

## Commands and outcomes

- `make -C go build`: passed, including the final native C ABI entry points.
- In `go/`, under `software-setup.sh --exec`, set `REDIS_SNAPSHOT_URI` to the
  verified endpoint and unset `REDIS_URI`, then run
  `go test -count=1 -p 1 -timeout=10m -run 'TestNativeReader|TestSharedSnapshot' .`:
  passed (127.598 s), including existing process-death/shared-view regressions.
- The same verified snapshot fixture with
  `go test -race -count=1 -p 1 -timeout=10m -run '^TestNativeReader' .`:
  passed (2.541 s), no race reports.
- `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 -timeout=10m ./...`
  from `go/`: all packages passed. Integration tests skip without endpoint
  variables; this unit run is not additional Redis compatibility evidence.
- Using the built library, source Python package and a temporary test venv,
  under `software-setup.sh --exec`:
  `python -m pytest -q ../python/tests/test_native.py ../python/tests/test_shared_snapshot.py`:
  **3 passed** (final run 1.30 s). This exercises the real Python/C/Go boundary,
  shared views across two tables, binary NUL/non-UTF8 data, exact int64 values,
  NULL/empty/missing fields, duplicates, quoted identifiers, rename/defaults,
  concurrent publications, and exception/closed-view cleanup.
- `git diff --check` and relative-link checks for new documentation: passed.

## Review and limits

Codex self-review followed the correctness/lifecycle, security/ownership, and
intent/evidence passes in REVIEW.md. No unresolved finding was identified for
the initial scope. This is not human approval or a production release.

The read path shares existing descriptor/index readiness proofs, backend fences,
strict row membership, version resolution, default/rename rules, and final lease
validation. Tests cover cancelled contexts, interrupted reads/Close, release of
a shared server pin, terminal expired views, and missing authoritative images.
The native permission test injects a backend connection refusal; it does not
replace an end-to-end management-API GRANT/REVOKE deployment test. New native
bindings were not separately tested through failover, resharding, or other
operating systems. No performance or drop-in client compatibility claim is made.

Application-key mappings, native Search, async Python cancellation, broader
command coverage, and packaging/release validation remain follow-up work. The
public contract documents raw storage bytes, internal row-ID addressing, explicit
ownership, serialization, response bounds and current exclusions.
