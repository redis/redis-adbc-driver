# Specification: Terra compatibility evidence

Date: 2026-11-10. Status: prepared for PR review.
See [intent](intent.md) and [plan](plan.md).

- Preserve the actual native Go ADBC connection result and source revision.
- Record RESP3/PING success, unavailable Search commands, and the failed driver
  initialization separately from the unexecuted benchmark workload.
- Keep query timings absent and completed query/ingestion counts at zero.
- Label the target Redis Terra; do not present it as verified 4x-QPF Redis
  Software evidence or a ClickBench run.
- Archive the compiled harness source and clearly identify its unreached paths
  and JSON failure semantics.
- Include client/server configuration, cost scope, and completed temporary
  resource cleanup without credentials, endpoint names, or infrastructure IDs.
- Preserve the existing VPC and Terra peering.

Acceptance evidence: [report](../../../../benchmarks/terra/2026-11-10/report.md)
and [sanitized results](../../../../benchmarks/terra/2026-11-10/results.json).
No driver behavior changes are included.

PR: [#370](https://github.com/redis/redis-adbc-driver/pull/370).
