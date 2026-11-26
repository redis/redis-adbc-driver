# Intent

Requested 2026-11-26 (UTC) in a development session: "look at the github issues and fix the tpc-ds items". This change covers [#696](https://github.com/redis/redis-adbc-driver/issues/696). Base: main `a609aab0`. See [spec](spec.md), [plan](plan.md), [validation](validation.md).

In the [SF1 Flex pass](../sf1-flex-suites/validation.md), TPC-DS Q75 hit the 60 s statement deadline. The driver reported it as:

```
UNKNOWN: [redis] context deadline exceeded
```

That is ADBC status `UNKNOWN`, with no SQLSTATE. Every other deadline in those runs, 8 in TPC-H and 14 in TPC-DS, came back as `TIMEOUT` with SQLSTATE 57014, which callers and the harness can classify.

Outcome wanted: a statement that ends because of its deadline or a cancel reports `TIMEOUT` or `CANCELLED` with SQLSTATE 57014, whichever worker pool observes it.

Scope:
- Error status mapping only. No change to plans, worker counts, budgets or results.
- The query's cost (Q75's timeout itself) is [#710](https://github.com/redis/redis-adbc-driver/issues/710), not this change.
