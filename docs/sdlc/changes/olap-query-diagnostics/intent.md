# Intent

Requested 2026-11-26 by the session that filed the SF1 Flex failure issues [#696](https://github.com/redis/redis-adbc-driver/issues/696)–[#717](https://github.com/redis/redis-adbc-driver/issues/717) after [#695](https://github.com/redis/redis-adbc-driver/pull/695). Base: main `7c83c486`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

Every one of those issues says the same thing under "Not known yet": the harness did not record the plan, worker grant, rows read or the server's RAM/flash object split, so the dominant cost of a slow or timed-out query could not be named. The issues also had to reconstruct by hand which queries ran before the failing one.

Outcome wanted: from now on, every Redis-backed query record in `benchmarks/olap-release/` carries:
1. the driver's execution diagnostics (plan or phase, worker grant, rows read, Redis command count and time, bytes read and allocated), from its existing `adbc.redis.execution_diagnostics_file` statement option;
2. on a Flex profile, the server's RAM/flash object split just before timing, from a Redis-side command the harness issues with `redis-py` over RESP3 (`run.py` does not call the management API);
3. the queries that ran before it in the pass.

Constraints:
- Public records stay sanitized: no index names, key prefixes, credentials, endpoints or SQL literals. Diagnostics plans contain literals, so fields are whitelisted, never copied.
- The coordinator still imports neither engine.
- Existing record fields and behavior stay unchanged.
- Tests fail first; a live smoke run uses a disposable Redis Software **Flex** fixture with verified 4x QPF only.

Out of scope: driver changes, new driver metrics, the management API, and fixing any of the SF1 failures.
