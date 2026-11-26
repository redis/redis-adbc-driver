# S3 collector deployment

Status: deployed 2026-11-19 (Pacific); see the [deployment README](README.md). The staging and migration steps below are the procedure that was used and are kept as a record. Before any further Terraform change, read [Terraform state reconciliation](#terraform-state-reconciliation).

The serverless collector uses API Gateway HTTP API, three Lambda functions, the existing private encrypted/versioned S3 bucket, a SecureString dashboard token, and EventBridge schedules. There is no database, NAT gateway, load balancer or provisioned Lambda concurrency. The dashboard shell is included in the API package; usage data is returned only by its authenticated summary endpoint.

Reports are durably buffered before HTTP acknowledgement. Summaries refresh every 15 minutes; download snapshots refresh daily. The dashboard displays the generation time. The source IP comes from API Gateway, not forwarded headers. Global throttling is shared across clients (10 requests/second, burst 20), replacing the legacy per-IP limits. Public feedback remains spoofable and unverified. Cold starts and slow S3 calls can exceed a driver's two-second deadline; retrying an already saved report is harmless.

## Build and stage

Use the existing Terraform backend and private operator variables. Keep all plans, packages, state exports, migration data, credentials and infrastructure identifiers out of git. The deployment requires AWS credentials configured privately. Build a package outside the checkout:

```sh
PYTHON=/path/to/venv/bin/python bash analytics/deploy/package-serverless.sh /private/path/collector.zip
```

The deployed package has the fixed timestamps and file modes this script writes. Its dependencies, resolved from the `requirements.txt` ranges, are boto3 and botocore 1.44.7, dnspython 2.9.2, redis 8.1.0, s3transfer 0.19.3, jmespath 1.1.0, urllib3 2.8.0, python-dateutil 2.9.0.post0 and six 1.17.0. `requirements.txt` does not pin these, so a later build can resolve newer versions and produce a different zip. Terraform's `source_code_hash` then updates all three functions. To plan without function changes, supply the exact deployed package.

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

Rolling back to EC2 is no longer a host restart. The legacy instance and its disk no longer exist (checked 2026-11-26). A return to EC2 would need a new host restored from a legacy RDB backup under `backups/` (while the 90-day backup rule still retains one) or from the migration baseline, plus a new Elastic IP, the nginx and collector configuration, and a reverted DNS cutover flag. Neither source contains reports received only by S3: reconcile those cumulative reports before claiming complete rollback. Keep S3 reports and the migration baseline throughout recovery. Never restore an older checkpoint in place without accounting for its published session high-water marks.

## Terraform state reconciliation

Checked 2026-11-26 with read-only AWS calls; no plan was run. The shared state holds the serverless module and the root legacy resources. Every `module.serverless[0]` resource matches the deployment, and the package in the operator's ignored `private/` directory has the deployed code hash. The root legacy resources differ from AWS:

| State address | In state | In AWS |
|---|---|---|
| `aws_instance.collector` | stopped | instance not found; root volume not found |
| `aws_ec2_instance_state.legacy[0]` | stopped | instance not found |
| `aws_security_group.collector` | present | exists, no attached network interfaces |
| `aws_iam_role.collector`, `aws_iam_role_policy.backup_writer`, `aws_iam_role_policy_attachment.ssm`, `aws_iam_instance_profile.collector` | present | exist |
| `aws_iam_user_policy.approved_collector_exception[0]` | present | exists; denies stop/terminate on the deleted instance |

`aws_eip.collector` is not in state; the migration released it. The state's DNS record is the alias form that `serverless_cutover=true` declares. The backup bucket's settings were not compared.

From the configuration, a plan would refresh away the missing instance. Because `main.tf` still declares `aws_instance.collector`, and `serverless_stop_legacy=true` declares `aws_ec2_instance_state.legacy`, it would then create a new legacy host from the pinned AMI with the legacy bootstrap user data, stop it again, and repoint the FinOps exception at the new instance. `prevent_destroy` blocks destruction, not creation. Do not plan or apply until this is resolved.

Options, each requiring the owner's explicit approval and a reviewed plan before any apply:

1. **Retire legacy compute in configuration (recommended).** Delete `aws_instance.collector`, `aws_ec2_instance_state.legacy`, `aws_eip.collector`, their variables and outputs, and the EIP branch of the DNS record. Add `removed` blocks with `lifecycle { destroy = false }` for the instance and instance-state addresses, so the plan only forgets them. The FinOps exception protects only that instance: remove it too, either destroyed or forgotten with its own `removed` block. Decide separately whether the security group, role and profile are destroyed or forgotten the same way.
2. **`terraform state rm`** of the instance and instance-state addresses. This edits shared state outside review and leaves the configuration unchanged, so the next plan still creates the host. Use it only together with option 1's configuration change, not instead of it.
3. **Keep an optional legacy host.** Gate the instance, instance state and FinOps exception behind a new count that defaults to zero, with `moved` blocks to their `[0]` addresses. Refresh then drops the missing instance and nothing is created. A future EC2 host would still start empty and need the restore described above.

`terraform apply -refresh-only` by itself removes the missing instance from state but leaves it declared, so the next normal plan still creates the host.
