# S3 telemetry collector

2026-11-19 · Requestor: Albert Wong · Source: 52b03c6f · Status: implemented and deployed

Replace the continuously running collector EC2 instance with request-driven AWS ingestion and scheduled dashboard summaries. The user chose S3 reports and summaries after comparing database options. Keep the driver's hostname, payload, two-second delivery timeout and opt-out behavior. Preserve historical telemetry and deployment IDs; do not introduce DynamoDB, RDS, or another running database.

See [specification](spec.md) and [plan and validation](plan.md). No issue or PR has been created for this change.
