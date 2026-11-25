# Intent

Date: 2026-11-25. Source baseline: `0ebee353`.
Status: implemented on a branch; awaiting human review, not merged.
Requested by the user through the TPC-DS follow-up issues.
Issue: [#660](https://github.com/redis/redis-adbc-driver/issues/660). See [spec](spec.md), [plan](plan.md), [validation](validation.md).

The analytical harness runs each engine and validator in a fresh child process. All of them share one client memory cgroup with the coordinator and `supervise.py`. When a worker exhausts that limit, the kernel can OOM-kill the coordinator too. The query record is then lost and has to be recovered by hand from checkpoints. TPC-DS SF1 Q4 did exactly that. The full Q1–Q99 run in [#649](https://github.com/redis/redis-adbc-driver/issues/649) needs a worker OOM to be recorded as that engine's failure by a process that survives.

Desired: a worker's OOM can kill only that worker, whether or not the run is supervised. The worker's limit can equal the recorded client limit, so results stay comparable with earlier runs.

Excluded: reducing query memory, changing driver behaviour, statement deadlines, validation rules or the default shared profile, and running any benchmark suite.
