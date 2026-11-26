# Specification

Date: 2026-11-26. Baseline: `7c83c486`.
See [intent](intent.md), [plan](plan.md), [validation](validation.md).

When a coordinator exits non-zero or exceeds the supervisor's 1200-second limit and left no `checkpoint-<query>.json`, `supervise.py` builds its fallback record as `{"suite", "query", "engine_isolation"}`. The `engine_isolation` value is the string `isolation.run_query` writes: `sequential fresh processes; separate disk-backed validation`. The supervisor then adds `coordinator_error`, `failed_phase` (`coordinator`), `worker_wall_seconds` and `client_oom_kills_delta` exactly as before.

Unchanged:

- A record built from an existing checkpoint, including its engine/phase status and error fields.
- `run_query`'s refusal of any `queries.jsonl` line without `engine_isolation`, and of a duplicate query record.
- Query order, the outer process-group kill, events and the final `complete` event.
- Coordinator isolation: neither the supervisor nor the coordinator imports DuckDB or the Redis ADBC driver.

A fallback record carrying `engine_isolation` states the harness mode of the pass in which the coordinator failed. It does not claim that any engine ran.

Reviewable implementation: [PR #719](https://github.com/redis/redis-adbc-driver/pull/719).
