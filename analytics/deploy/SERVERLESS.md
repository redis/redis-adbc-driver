# S3 collector deployment

The serverless collector uses API Gateway HTTP API, three Lambda functions, the existing private encrypted/versioned S3 bucket, a SecureString dashboard token, and EventBridge schedules. There is no database, NAT gateway, load balancer or provisioned Lambda concurrency. The dashboard shell is included in the API package; usage data is returned only by its authenticated summary endpoint.

Reports are durably buffered before HTTP acknowledgement. Summaries refresh every 15 minutes; download snapshots refresh daily. The dashboard displays the generation time. The source IP comes from API Gateway, not forwarded headers. Global throttling is shared across clients (10 requests/second, burst 20), replacing the legacy per-IP limits. Public feedback remains spoofable and unverified. Cold starts and slow S3 calls can exceed a driver's two-second deadline; retrying an already saved report is harmless.

## Build and stage

Use the existing Terraform backend and private operator variables. Keep all plans, packages, state exports, migration data, credentials and infrastructure identifiers out of git. The deployment requires AWS credentials configured privately. Build a package outside the checkout:

```sh
PYTHON=/path/to/venv/bin/python bash analytics/deploy/package-serverless.sh /private/path/collector.zip
```

Store the package under the ignored Terraform `private/` directory and persist operator settings in an ignored `operator.auto.tfvars.json` (mode 0600). Never put AWS keys or dashboard tokens in that file. Supply `serverless_package` with that absolute path; set `legacy_ami_id` to the current host's AMI so the public AMI lookup cannot replace it during migration. Leave `serverless_cutover=false` and `serverless_schedules_enabled=false`. Temporarily enable `serverless_migration_access` only for the migration. Review the plan for unrelated changes and preserve existing private operator values.

Terraform creates the new API and TLS certificate without switching the public A record. Its staging URL is a private operational output. Test the dashboard shell, unauthenticated summary rejection, invalid/oversized payload rejection and ingestion storage failures. Do not generate synthetic hosted usage unless deliberately classified as test traffic.

## Migrate without losing the overlap

1. Take and verify a final legacy RDB/S3 backup. Preserve legacy configuration privately for rollback.
2. Redirect the legacy nginx ingestion location to the staging API with TLS verification, SNI and the upstream Host header. Keep the original IP in neither client-controlled JSON nor a trusted forwarded header; reports arriving via this temporary bridge observe the legacy proxy's public IP. Direct traffic after DNS cutover has its original API Gateway observed IP. Remove nginx's old upstream write-token injection for this location.
3. Stop the old collector and any download writers. The nginx bridge continues accepting reports into S3 while the legacy database becomes quiescent. Do not shut Redis down before export.
4. On the legacy host, load its environment privately, set `ANALYTICS_BUCKET`, and set `ANALYTICS_MIGRATION_REDIS_URL` to an authorized local administrative connection. The normal collector ACL intentionally lacks the inventory commands required by export. Use the existing root-readable ACL configuration; never print its secrets.
5. Run the packaged `migrate_serverless.py --quiesced` using a Python environment containing the package dependencies. It conditionally creates `serverless/baseline.json.gz`; it refuses an existing baseline or already published manifest. Copy the existing dashboard token into the configured SSM SecureString without printing it or putting it in Terraform state.
6. Invoke the refresh Lambda manually. Compare historical counters, deployment aliases/IDs, download snapshots and recent execution identities against the baseline. Inspect duration, memory and summary timestamps before enabling its recurring schedule. A budget failure blocks cutover; increase capacity or redesign aggregation rather than discard history.
7. Switch `serverless_cutover=true` and enable schedules. Verify the unchanged public hostname: shell works, summary rejects a missing/bad token, valid reads show imported history, and ingestion acknowledges within the driver deadline on representative requests. Do not count an accepted queue write as successful summary processing.
8. Keep nginx bridging long enough for DNS caches and in-flight requests to drain. Set `serverless_stop_legacy=true` only after public endpoint and scheduled-worker checks pass. This stops the legacy EC2 host and releases its unused Elastic IP. Retained EBS storage still incurs charges; deleting the disk is a separate retirement step. Remove temporary migration IAM access.

## Recovery, limits and costs

Raw reports and the baseline have no automatic expiration. Session high-water marks let the worker list the raw inventory without downloading accepted reports again. A checkpoint retains exact historical deltas for the current rolling-window and retention definitions. There are explicit object, pending-byte, checkpoint-byte and remaining-time budgets; crossing one fails the refresh and leaves the prior complete summary available. Inspect the worker error alarm and `generated_at` for stale dashboards.

The manifest references complete immutable state and summary objects. A successful refresh keeps the current and previous generations and retires the older referenced generation. The bucket expires noncurrent retired generation versions after seven days; it does not expire live reports or baselines. Failed workers or failed best-effort cleanup can leave orphan generation objects; inventory those before deleting anything, protecting all manifest references. The raw log is authoritative for recovery. Rebuilding from the baseline must replay report objects in original receipt order; never blindly sum cumulative payloads.

Costs depend on report frequency, payload size, history, API reads and worker duration. A fixed monthly estimate is not implied. S3 requests/storage, scheduled processing, Route 53 and any retained legacy resources continue to cost money. No RDS, DynamoDB or EC2 compute is needed for normal serverless collection.

To roll back, first disable `serverless_stop_legacy` and start the legacy host if stopped, reallocate an Elastic IP through Terraform, restore its saved nginx configuration and collector service, then revert the DNS cutover flag. This restores historical legacy service but does not import reports received only by S3: reconcile those cumulative reports before claiming complete rollback. Keep S3 reports and the migration baseline throughout recovery. Never restore an older checkpoint in place without accounting for its published session high-water marks.
