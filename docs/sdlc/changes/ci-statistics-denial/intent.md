# Intent: statistics denial CI regression

Date: 2026-10-22. Requestor: Albert T. Wong. Author: Codex.
Source: `f9045da`. Status: implemented; verification in [plan](plan.md).

Investigate the [failed Test run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39852041663)
and [release run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39852041316).
The statistics ACL regression expects Unauthorized although shared Redis command
errors use I/O and preserve NOPERM. Preserve explicit failure and absence of
fabricated statistics. See [spec](spec.md).

The [RESP3 run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39852070916)
also read ACL recipes from README after they moved to the driver reference.
Current main already reads the driver reference; retain that correction.

Follow-up: merged PR #493 cleared those failures, but exposed a later TLS
Cluster resharding failure. Investigate it in
[PR #494](https://github.com/alberttwong/redis-adbc-driver/pull/494), keeping raw
worker errors private and exact row/schema assertions intact.
