# Plan

Date: 2026-11-26. Baseline: `7c83c486`.
See [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Add `benchmarks/olap-release/test_supervise.py`. It copies `supervise.py` next to a stub `run.py` and points the client OOM counter at a synthetic `memory.events` file. The stub exports three queries; query 1 exits the way a Flex compaction refusal does, before any checkpoint. Queries 2 and 3 run the real `isolation.run_query` with engine workers that report success. No Redis, DuckDB or benchmark SQL is used.
2. Run the test against the unchanged `supervise.py` and record the refusal of queries 2 and 3.
3. Add `engine_isolation` to the supervisor's checkpoint-less fallback record.
4. Note the behavior and the new test in `benchmarks/olap-release/README.md`.
5. Run `python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'`.
6. Open a PR against `main`; human review remains pending.
