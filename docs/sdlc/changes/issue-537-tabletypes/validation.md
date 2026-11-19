# GetObjects table-type filtering: validation

Date: 2026-11-19. Source: `3e45955b` plus this change.
Status: focused verification passed; maintainer review pending.
Links: [intent](intent.md), [specification](spec.md), [plan](plan.md),
[issue #537](https://github.com/redis/redis-adbc-driver/issues/537).

## Environment and results

Native host shared library built with `make -C go build`. Validation dependencies
were resolved with `uv sync --project go/validation`, using Python 3.14.10,
adbc-driver-manager 1.13.0 and upstream validation revision
`e4c18a5ccf74dec1efd5b1b11fc6fedd35dc349d`.

Tests used the exclusively leased disposable Redis Software fixture. The owning
checkout's `software-setup.sh --exec` wrapper verified Redis 8.8.0, three primary
shards, active 4x QPF, required modules and retained-control persistence policy
before execution. No fixture endpoint was used concurrently by another worker.
Operational artifacts remain in ignored build directories.

- `pytest -q -ra tests/test_connection.py::TestConnection::test_get_objects_tabletypes_filter tests/test_getobjects_tabletypes.py`: **3 passed, 0 skipped**.
  Tests load this checkout's shared driver through the Python driver manager and
  consume the native Arrow metadata streams completely.
- `REDIS_RBAC_TEST_API=... go test -count=1 -run '^TestSQLRBACSelect$/^metadata_with_absent_view_directory$' -v .`:
  **PASS**, including type filters with a real restricted Software reader,
  denied SELECT before/after discovery, and unchanged management ACL write count.
- `python3 -m py_compile` on changed Python files and `git diff --check`: passed.

An initial Go test compile failed because the added test passed the type list in
the column-filter argument position; the invocation was corrected and rerun
successfully. There was no runtime filtering failure and no runtime modification.

## Review and limits

Self-review covered nested table membership, full stream consumption, reader
release, fixture cleanup and metadata ACL behavior. No actionable findings remain
from that review. This is not maintainer approval. The complete upstream and Go
suites were not run for this isolated change; final integration checks belong to
the combined issue branch. No benchmark, other platform, release or unsupported
server claim is made.
