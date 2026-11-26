# Retire the legacy EC2 collector from Terraform

2026-11-26 · Requestor: Albert Wong · Source: 5dbf4056 · Status: configuration changed; not applied

The hosted collector has served from API Gateway, Lambda and S3 since the 2026-11-19 cutover. The legacy EC2 instance and its disk no longer exist, but the shared Terraform state still records the instance as stopped. While `main.tf` declared it, any plan would propose creating a new legacy host. The owner chose to retire the legacy compute in configuration and delete its unused leftovers on the next approved apply.

Out of scope: applying the change, editing the shared state directly, changing the serverless collector, and deleting the historical `bootstrap.sh`, `update.sh` and legacy RDB backups.

See [specification](spec.md) and [plan and validation](plan.md). Background: [serverless source recovery](../serverless-telemetry/plan.md#source-recovery-2026-11-26).
