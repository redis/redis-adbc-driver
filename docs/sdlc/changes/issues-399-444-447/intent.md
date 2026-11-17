# Intent

Date: 2026-11-17. Requestor: Albert Wong. Author: Codex.
Source: 3671fca7. Status: investigation and review proposal; no performance claim.

Work on [#399](https://github.com/redis/redis-adbc-driver/issues/399),
[#444](https://github.com/redis/redis-adbc-driver/issues/444),
[#445](https://github.com/redis/redis-adbc-driver/issues/445),
[#446](https://github.com/redis/redis-adbc-driver/issues/446), and
[#447](https://github.com/redis/redis-adbc-driver/issues/447).

Consumers need an explicit unmanaged replacement ownership contract and evidence
for improving fixed SQL/ADBC costs. Albert selected a proposal-only deliverable
for #399 on 2026-11-17: keep runtime replacement unsupported. Performance work
must retain snapshots, ACLs, exact values, deterministic ownership and unknown
outcome handling. No unsupported server or ClickBench work is included.

See [spec](spec.md), [plan](plan.md), and [evidence](validation.md).
