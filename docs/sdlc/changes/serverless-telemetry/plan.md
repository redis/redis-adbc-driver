# Implementation and verification

See [intent](intent.md) and [specification](spec.md).

1. Extract storage-independent session validation, identity association and dashboard summarization from `analytics/collector.py`.
2. Add conditional S3 ingestion, incremental checkpoint updates, scheduled summaries/downloads, PTR caching and authenticated API routes in `analytics/serverless.py`.
3. Add a create-only migration export, arm64 Lambda packaging, staged Terraform resources and rollback instructions. Preserve the existing backup bucket and legacy host through verification.
4. Check failure recovery, sequence deduplication, authentication/privacy, pagination, filters and Redis-to-S3 migration parity. Run database-backed regressions only after `make -C go redis-up` verifies active 4x QPF.
5. Stage AWS resources, migrate a quiesced baseline, check counts/IDs/authentication and delivery latency, switch DNS, enable schedules, verify the public endpoint and stop the legacy collector host. Remove temporary migration privileges.

Validation: 28 Python tests passed with no skips against Redis Software after `make -C go redis-up` verified active 4x QPF, plus five dashboard tests. Terraform validation passed. Staging and cutover plans were checked for unrelated replacement or destruction before applying.

Live verification on 2026-11-19 (Pacific): the streaming baseline preserved 125,031 reports and 90,649 sessions. Every traffic filter matched the legacy counters, versions, retention values, deployment IDs/counts and recent execution identities. The first refresh completed in approximately 37 seconds end to end (35 seconds function duration, 428 MiB peak memory). A scheduled refresh was observed with zero worker errors or throttles. The daily download function also completed successfully. Public shell/health/authenticated summary returned 200; missing or bad read tokens returned 401. An existing report replay returned 200 and its duplicate returned accepted=false without changing counters. The warm replay took 0.3 seconds; the first took 3.2 seconds, so the documented two-second client-timeout limitation remains.

The public DNS record now points to API Gateway, schedules are enabled, temporary migration privileges are removed, legacy compute is stopped and the unused Elastic IP is released. Final configuration pins the legacy AMI and ignores the observed public-address association flag so address retirement cannot recreate the retained host. The legacy encrypted disk remains for rollback and still incurs storage charges. A final infrastructure plan reported no changes, and public shell/health/authenticated summary checks passed after the legacy host was stopped. Temporary local AWS credentials and the local migration-data copy were removed. No issue, PR or release was created; repository changes remain available for review. Operational identifiers, credentials, data exports, plans, state snapshots and packages remain private. This records agent verification, not independent human approval.

## Source recovery (2026-11-26)

The migration session deployed this change without committing it. Its source survived only in a local stash (work-tree commit `d72fb680`, index `899c1575`) and in an untracked-file backup in the main checkout; the two copies were identical. This change restores the `analytics/` paths and these records from that stash, without the unrelated benchmark and documentation files the stash also held. Main had not changed `analytics/` since the stash base.

Deployed comparison, using read-only AWS calls:

- The three functions run one package (identical code hash, last modified 2026-11-20 01:38 UTC). Its `collector.py`, `serverless.py`, `downloads.py`, `migrate_serverless.py` and dashboard files are byte-identical to this tree. Its 2,512 dependency files match their wheel `RECORD` hashes; the versions are listed in [SERVERLESS.md](../../../../analytics/deploy/SERVERLESS.md#build-and-stage). A byte-for-byte rebuild was not completed because this session's network policy blocks the PyPI file host.
- Runtime, architecture, handlers, memory, timeouts, reserved concurrency and environment variable names match the module. So do the API routes, integration, stage throttling, disabled access logs, custom-domain endpoint and TLS policy, both schedules (enabled), each function's inline IAM statements, 14-day log retention and the alarm without actions.
- The only difference found: `requirements.txt` ranges do not pin the deployed dependency versions, so a fresh build may resolve newer ones.

The migration record above says the legacy disk remains for rollback. That is no longer true: the legacy instance and its root volume no longer exist, while Terraform state still records the instance as stopped. See [Terraform state reconciliation](../../../../analytics/deploy/SERVERLESS.md#terraform-state-reconciliation) for the discrepancy and the options; none has been applied.

Validation:

- `python -m unittest discover -s analytics -p 'test_*.py' -v` with Python 3.12.15 and the deployed dependency versions: 28 passed, 0 skipped. It ran against Redis Software 8.8.0 (3 shards) from `make -C go redis-up` on a dedicated Compose project and ports, after the management API reported active QPF scaling factor 4, 6 Search workers, `mnp` routing and 32 connections. Without `ANALYTICS_TEST_REDIS_URL`, 14 of the 28 skip.
- `node analytics/test_dashboard.cjs`: 5 passed.
- `terraform validate` after an offline `terraform init -backend=false` with the locked AWS provider 6.73.0: passed. `terraform fmt -check -recursive`: no changes. No plan or apply was run.
- The Go `TestUsage` checks were not run; no driver code changed.

This records agent verification, not independent human approval.

The state discrepancy is resolved in configuration by [retire-legacy-collector](../retire-legacy-collector/intent.md); it has not been applied.
