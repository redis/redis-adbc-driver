# Validation

Baseline: [last green Test run](https://github.com/redis/redis-adbc-driver/actions/runs/44393488934), Linux amd64/arm64 with Redis Software and verified 4x QPF. The slowest driver job took 18m16s; snapshot jobs took 15m26s/17m40s. C bridge checks consumed 2m29s/2m46s before ordinary Go execution, and ordinary Go steps took 9m15s/11m04s. These are historical hosted CI timings, not a new benchmark.

Local Go discovery found 1,660 runnable top-level tests/examples/fuzz functions in the ordinary scope and 416 in the snapshot scope. Replaying the historical observed test times through the new assignment gives ordinary shard loads of approximately 192/189/195 seconds and snapshot loads of 193/192/194/194 seconds. These estimates exclude compilation, linking, fixture setup, runner queues and validation jobs; they are not measured PR latency.

Local checks: shard partition/discovery/command/report contracts, upstream failure/cancellation/skip gate checks, existing architecture selector checks, actionlint, Python compilation and strict documentation build. Hosted measurements will be attached to the PR.
