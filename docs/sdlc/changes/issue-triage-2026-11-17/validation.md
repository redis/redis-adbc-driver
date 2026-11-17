# Validation and review

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Driver source: `30a9eefbf13d19b3e3129171f8ecdc5579b852a2`.
Status: verification complete; documentation PR supplies the delivery record.
[Intent](intent.md), [spec](spec.md), [plan](plan.md), [disposition](triage.md),
[sanitized results](results.json).

## Current-source checks

Owned Redis Software / Redis 8.8.0, three primary shards, noeviction,
authenticated trusted TLS/RESP3. `make -C go redis-up` initialized the fixture;
each test invocation repeated the management gate via `software-setup.sh --exec`:
active QPF factor 4, six Search workers, mnp routing and 32 connections.
Client: macOS arm64 through the Docker-published endpoint. Operational identifiers,
raw logs, credentials and trust material are excluded from this record.

- Focused `make -C go test-software` with `-race -json -count=1 -p 1` and selection
  `Test(Matview|ACLMatview|SQLNamedIndexAndMatView|SQLNewFeatureErrorsAndMetadata|SQLAutoIngestOwnershipAndFailedRefresh)`:
  **12 top-level passes, three passing cleanup subtests, one top-level skip,
  zero failures**. The skipped `TestACLMatviewRetirementOpenRecovery` requires
  ACL operations blocked by Software. Other packages have no matching tests;
  their empty runs are not counted as coverage.
- The 25,000-row regression drains and validates every old value after a second
  connection changes the source and refreshes concurrently; fresh reads validate
  the new 50-row generation. Multiple generations, lease/recovery, publication
  races, cleanup, ownership fences, failed-refresh and SQL lifecycle checks pass.
- `make -C go build`: passed with the version linker flag. Go 1.27.4,
  pinned Arrow Go v18.9.0; no dependency override or production change.
- Direct interpreter, persistent isolated environment:
  `python -m pytest -q -ra --tb=short python/tests/test_ray_datasource.py` with
  `RUN_RAY_TESTS=1 REQUIRE_RAY_TLS=1`: **20 passed, zero failures/skips**, 20.13 s.
  Python 3.14.10, Ray 2.60.0, PyArrow 26.0.1. Covers worker execution/types,
  snapshots, explicit release and retry, native cancellation, Ray task retry,
  empty results and missing worker CA rejection.
- Initial Ray launch via `uv run` stalled after three cases because worker
  startup could not resolve an injected `--with` requirement; it was interrupted
  and is not a pass. An attempted direct ephemeral interpreter then no longer
  existed after uv exited. The complete passing run uses a persistent venv and
  direct interpreter with uv runtime injection disabled. No test was weakened.
- Relative Markdown targets and `git diff --check`: passed. GitHub REST verified
  every listed upstream PR's state/draft/merge fields. The built-site checker
  was not used: these records/status pages are outside the selected site build;
  its initial invocation also lacked PyYAML in the host interpreter.

No full Go/native suite, new benchmark, sanitizer qualification, multi-node Ray
run, installed release-wheel or universal transport result is claimed. Passing
focused tests does not resolve #217/#265 EOF diagnostics or #446/#460 ownership
qualification. Earlier measurements retain their actual distributions/settings.

## Disposition and review

Closed #105 after the reader guarantee fix merged in #289 and passed the current
race selection. dbt acceptance remains in redis/redis-dbt-adapter#38/#39 per the
explicit prior scope decision. PostgreSQL CONCURRENTLY unique-index validation
is still outside the documented driver behavior.

Closed #164 after the external adapter merged in #231, Ray's maintainer selected
the external community-listing route, upstream #67560 merged on November 9, and
the current worker suite passed. This is not a generic upstream Ray ADBC feature
or endorsement. Wider deployment/performance remains outside this supported scope.

#236/#249 retain outstanding upstream outcomes; status now records four merges,
twelve non-draft open PRs and two closed-without-merge contributions. The full
53-issue table retains all other closure gates, including deferred scope.

Agent review followed REVIEW.md for correctness/lifecycle, ownership/security,
and evidence/claim scope. No production behavior changed. No human approval or
hosted CI result is asserted. Only the owned fixture was removed; pre-existing
containers were preserved. Generated test credentials and trust were removed.
