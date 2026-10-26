# Plan: partitioned Dask reads

Date: 2026-10-20. Source: `faa03dd`. Author: Codex. Status: completed locally.
[Intent](intent.md), [spec](spec.md), [validation](validation.md),
[issue #353](https://github.com/alberttwong/redis-adbc-driver/issues/353).

1. Verify bound range predicates and existing DB-API lifecycle in the repository.
2. Add `python/adbc_driver_redis/dask.py` and opt-in/test dependencies.
3. Add `python/examples/dask_read.py` and lifecycle/distributed tests.
4. Document worker setup, explicit schema/partitions, SQLAlchemy distinction,
   consistency and memory limits in the integration guide and README links.
5. Run focused tests, the standalone example, minimum-version compatibility and
   relevant existing Python regressions against an isolated Redis with Search.
6. Review correctness/lifecycle, data ownership and acceptance coverage using
   `REVIEW.md`; record actual evidence without claiming human approval.

Unrelated DuckDB, workflow, Makefile and submodule changes predate this task and
are outside its scope. No changes to their contents are needed.

Minimum-version verification rejected Dask 2024.4.0 because it failed during
import on Python 3.12.14; Dask 2024.5.0 passed and is the declared minimum.
