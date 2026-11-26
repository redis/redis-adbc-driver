# Intent

Date: 2026-11-26. Source baseline: `7c83c486c6388cae80bf8e183e61272dbb7d6893`.
Status: implemented locally; awaiting human review, not merged or released.
Found while adding per-query diagnostics in [PR #718](https://github.com/redis/redis-adbc-driver/pull/718), which this change does not require.
See [spec](spec.md), [plan](plan.md), [validation](validation.md).

In the isolated OLAP harness (`benchmarks/olap-release/`), one query coordinator that exits before `run_query` writes its checkpoint makes every later query in the pass fail. An example is `run.py` refusing to time a query on Redis Software Flex (`flex_compaction_refused`). `supervise.py` then appends a fallback failure record without `engine_isolation`. The next coordinator's `run_query` takes that record for historical shared-worker output and refuses with "use a new output directory; historical shared-worker results must remain separate". The rest of the pass records that misleading error instead of running.

Desired behavior: the refused query keeps its coordinator-failure record, and later queries in the same pass run normally. Genuinely historical shared-worker output must still be refused.

Out of scope: why a coordinator fails, the Flex compaction gate itself, the `pass_order` diagnostics in PR #718, timeouts and engine/validation behavior. No benchmark suite or live database is run for this change.
