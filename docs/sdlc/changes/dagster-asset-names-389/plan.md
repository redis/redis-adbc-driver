# Plan and verification: Dagster asset names

Date: 2026-11-12. Author: Codex; requestor: repository user. Base: `8d4dcacd`.
Issue: [#389](https://github.com/redis/redis-adbc-driver/issues/389). Status: implemented and verified; awaiting human review.

See [intent](intent.md) and [spec](spec.md).

1. Capture failing name-mapping cases before implementation.
2. Implement shared reversible naming in python/adbc_driver_redis/dagster.py.
3. Add isolated mapping tests and independent read/write regression coverage.
4. Document mapping and migration in docs/integrations/dagster.md.
5. Run focused tests; Redis checks require make -C go redis-up and verified
   active Software 4x QPF. Review correctness, ownership, and compatibility.

## Evidence

- Before implementation: isolated mapping tests had 13 failures, 8 passes;
  both Software read/write regression cases returned the second asset's value
  when reading the first (2 failures). An initial attempt hit a stale shared
  library's CA handling; rebuilding with `make -C go build` resolved it before
  capturing the actual overwrite failures.
- After implementation: all 35 tests in `python/tests/test_dagster.py` passed,
  with no skips, using Python 3.14.10 and Dagster 1.13.29.
- Server: Redis Software 8.8.0, three shards. `make -C go redis-up` and
  `software-setup.sh --exec` verified active 4x QPF, six Search workers, mnp
  routing, 32 connections, and noeviction before live tests.
- Exact test invocation from `go/` (library built with `make -C go build`):
  `bash software-setup.sh --exec env PYTHONPATH=../python REDIS_ADBC_LIBRARY="$PWD/build/libadbc_driver_redis.dylib" /tmp/redis-adbc-389-py314/bin/python -m pytest -q ../python/tests/test_dagster.py --tb=short`
- `git diff --check` passed. Documentation examples match asserted mapping
  cases; local change-record links resolve.

## Review

Codex inspected correctness/lifecycle, data ownership, and intent/design per
REVIEW.md. Encoded names contain only lowercase ASCII and keep full component
boundaries; reserving the marker prevents encoded/literal aliases. Reads and
writes share the mapping; no legacy-name fallback can drop or read an unrelated
asset. Existing staging and partition behavior remains covered by the suite.
No actionable findings remain in this review. Main compatibility risk: affected
assets use new targets and need explicit migration or rematerialization, as
shown in the integration guide. No broader Go suite was run for this Python-only
behavior change. No human review or merge approval is claimed.
