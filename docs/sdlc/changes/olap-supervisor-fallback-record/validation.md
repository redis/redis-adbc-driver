# Validation and review

Date: 2026-11-26. Baseline: `7c83c486c6388cae80bf8e183e61272dbb7d6893`.
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Reproduced before the fix

With the new `test_supervise.py` and the unchanged `supervise.py`, on local macOS with Python 3.15.2:

```sh
python3 -m unittest discover -s benchmarks/olap-release -p 'test_supervise.py' -v
```

`test_coordinator_refused_before_checkpoint_does_not_poison_later_queries` failed. After query 1's synthetic refusal, the coordinators for queries 2 and 3 each raised `ValueError: use a new output directory; historical shared-worker results must remain separate` from `isolation.run_query`, so the supervisor recorded coordinator failures for them as well.

## Fixed harness

The same test passed. It checks that:

- query 1's record has `coordinator_error` "coordinator exited with code 1", `failed_phase` `coordinator`, `client_oom_kills_delta` 0, a `worker_wall_seconds` value and no other fields besides `suite`, `query` and `engine_isolation`;
- that `engine_isolation` equals the value `run_query` wrote for queries 2 and 3, which both recorded `ok` for DuckDB and Redis;
- no checkpoint remains, and `events.jsonl` has the refusal and the final `complete` event;
- a bare historical record appended afterwards is still refused by `run_query` before any engine starts.

Full harness suite:

```sh
python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'
```

Ran 65 tests in 5.821 s: OK, 1 skipped. The skip is `test_isolation.WorkerMemoryCgroupTests.test_worker_oom_is_contained_and_recorded`, which runs only with `OLAP_HARNESS_CGROUP_TESTS=1` inside a disposable Linux container; this change does not touch it. The analytical-harness CI workflow runs the same discovery command under Python 3.14; this local result does not claim a CI run.

## Limits

The test uses a stub coordinator, so it does not exercise `run.py`'s real Flex recheck; `test_flex_compaction.py` covers that refusal separately. No Redis Software Flex database, benchmark query or container was run. This is an agent check, not human maintainer approval.

Reviewable implementation: [PR #719](https://github.com/redis/redis-adbc-driver/pull/719).
