# Intent

Date: 2026-10-29. Initial source: `95d6583`; PR base: `c997337`.
Status: ported for a pull request in `redis/redis-adbc-driver`.
Requested by the user; implemented by Codex.

Test the Redis driver with pandas using the native ADBC connection. Preserve
repeatable integration coverage and a runnable example. No driver behavior
changes or Prefect integration are in scope.

Port source: [original PR #517](https://github.com/alberttwong/redis-adbc-driver/pull/517),
commit `b38c9f2cd771769c865d23e549ccc0d9ac470a0d`. Redis port base:
`71625e153e5b682715e99be1052f5dcaec689a49`. The port preserves existing pandas
read coverage and adds the source tests as `python/tests/test_pandas_writes.py`.

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
