# S3 telemetry collector

2026-11-19 · Requestor: Albert Wong · Source: 52b03c6f · Status: implemented and deployed

Replace the continuously running collector EC2 instance with request-driven AWS ingestion and scheduled dashboard summaries. The user chose S3 reports and summaries after comparing database options. Keep the driver's hostname, payload, two-second delivery timeout and opt-out behavior. Preserve historical telemetry and deployment IDs; do not introduce DynamoDB, RDS, or another running database.

See [specification](spec.md) and [plan and validation](plan.md). The deployed source was committed to the repository on 2026-11-26 in [PR #721](https://github.com/redis/redis-adbc-driver/pull/721), after a byte comparison with the running functions; see [source recovery](plan.md#source-recovery-2026-11-26).
