# AWS analytics deployment

Status (checked 2026-11-26): since the 2026-11-19 (Pacific) cutover, the hosted
collector at `https://adbc-telemetry.sa.redislabs.com` runs on API Gateway and
AWS Lambda with S3 storage. No EC2 host, Redis server or other database is
involved. The driver's endpoint, payload, two-second delivery timeout and
opt-out behavior are unchanged. Historical telemetry and the existing dashboard
token were migrated. See the [serverless deployment and recovery guide](SERVERLESS.md)
and the [status change record](../../docs/sdlc/changes/analytics-serverless-status/plan.md).

The deployed code is in this repository: [`serverless.py`](../serverless.py) (all
three functions), [`collector.py`](../collector.py) (shared validation and
summaries), [`migrate_serverless.py`](../migrate_serverless.py),
[`package-serverless.sh`](package-serverless.sh), and the Terraform in
[`terraform/serverless.tf`](terraform/serverless.tf) and
[`terraform/serverless/`](terraform/serverless/main.tf). A 2026-11-26 read-only
comparison found the deployed application files byte-identical to these sources
and the deployed function, API, schedule, IAM, log and alarm settings matching the
module. See the [source recovery record](../../docs/sdlc/changes/serverless-telemetry/plan.md#source-recovery-2026-11-26).

> [!WARNING]
> Do not run `terraform plan` or `apply` against the shared state until the
> legacy instance is reconciled. The state still records the deleted
> `aws_instance.collector` as stopped, and `main.tf` still declares it, so a plan
> would propose creating a new legacy collector host. See
> [Terraform state reconciliation](SERVERLESS.md#terraform-state-reconciliation).
> Since 2026-11-26 the functions also run a package built outside Terraform (the
> [dashboard exclusion filters](../../docs/sdlc/changes/dashboard-exclusion-filters/plan.md)).
> Planning with an older `serverless_package` would revert all three functions'
> code; supply the package whose SHA-256 matches the deployed `CodeSha256`.

## Open the dashboard

1. Go to <https://adbc-telemetry.sa.redislabs.com/>.
2. Enter the dashboard read token. It is stored as the SSM SecureString parameter
   `/redis-adbc-analytics/read-token` in account `735486936198`, region
   `us-east-1`. Read it with your own approved credentials and do not paste it
   into tickets, chat or commits:

   ```sh
   aws ssm get-parameter --profile YOUR_APPROVED_PROFILE --region us-east-1 \
     --name /redis-adbc-analytics/read-token --with-decryption \
     --query Parameter.Value --output text
   ```

3. Summaries are regenerated every 15 minutes, not on each page load. The
   dashboard shows when the summary it displays was generated. An old time means
   the refresh worker is failing; the previous complete summary stays visible.
4. To hide traffic, choose **All except CI, inferred CI and benchmark** or **All
   except CI (labeled and inferred)** under *Reporting traffic*. To hide rows in a
   table, prefix a search or filter term with `-` (for example `-runnervm`);
   quote phrases with spaces (`-"ci / test"`).

The read token was carried over from the EC2 collector. Its old location,
`/etc/redis-adbc-analytics/collector.env` on the EC2 host, no longer exists.

## Resources

| Component | Configuration |
|---|---|
| Hostname | Route 53 alias record for `adbc-telemetry.sa.redislabs.com` to an API Gateway custom domain with an ACM certificate |
| API | HTTP API, `$default` stage, Lambda proxy integration (10-second timeout). Routes: `GET /`, `/dashboard.js`, `/dashboard.css`, `/health`, `/api/summary` and `POST /v1/usage` |
| Throttling | Shared by all clients: 10 requests/second, burst 20. It replaces the EC2 proxy's per-IP limits |
| Lambda functions | Python 3.12 on arm64, named `redis-adbc-telemetry-s3-<role>`: `api` (256 MiB, 10 s, reserved concurrency 10), `refresh` (2048 MiB, 900 s, concurrency 1) and `downloads` (256 MiB, 300 s, concurrency 1) |
| Schedules | EventBridge: `refresh` every 15 minutes, `downloads` daily |
| Storage | Existing private bucket `redis-adbc-telemetry-735486936198-us-east-1` (versioned, encrypted, HTTPS-only). Raw reports and the migration baseline (`serverless/baseline.json.gz`) have no expiry. Retired summary generations expire 7 days after becoming noncurrent. Legacy `backups/` objects expire after 90 days |
| Dashboard token | SSM SecureString `/redis-adbc-analytics/read-token`, read by the `api` function |
| Monitoring | CloudWatch alarm on `refresh` errors. No notification action is configured, so check it explicitly |

## Behavior and limits

- Public ingestion needs no client secret. The `api` function validates the
  payload allowlist and 64 KiB maximum. It acknowledges a report only after a
  conditional S3 write succeeds or the session/sequence object already exists, so
  a duplicate returns `accepted: false` without changing counters.
- The observed source IP comes from API Gateway's request context. Forwarded
  headers and client JSON cannot supply it. Reports remain self-reported and
  unverified.
- A cold start or slow S3 call can exceed the driver's two-second timeout.
  Retrying a report that was saved but not acknowledged is safe.
- Throttling is global, so a single noisy client can consume the shared limit.
- Raw report history has no expiry, and storage plus worker cost grow with it.
  Worker size and time guards fail closed and keep the previous summary.
- Every traffic filter, including the two exclusion presets, is precomputed into
  one `summaries.json` per generation, which the `api` function reads on each
  dashboard request. On 2026-11-26 the eight filters made a 9.49 MB file, a
  66-second refresh (529 MB peak) and an `api` peak of 169–174 MB of 256 MB.
  Adding filters raises all three; raise `api` memory before adding more.
- Costs are usage-based (API requests, Lambda duration, S3 requests/storage,
  Route 53). There is no fixed monthly compute charge.
- API access logs are disabled.

Migration verification preserved 125,031 historical reports and 90,649 sessions,
with all traffic-filter totals, deployment IDs and recent execution identities
matching the old collector. A scheduled refresh and authenticated public dashboard
reads succeeded. A warm ingestion replay took about 0.3 seconds; its first request
took about 3.2 seconds, exceeding the driver's two-second timeout despite durable
storage. Duplicate retries remain safe.

## Checks

```sh
curl --fail https://adbc-telemetry.sa.redislabs.com/health
# Expect 401 without the dashboard token:
curl -o /dev/null -w '%{http_code}\n' https://adbc-telemetry.sa.redislabs.com/api/summary
# Expect 200 with the token, without printing it:
t="$(aws ssm get-parameter --profile YOUR_APPROVED_PROFILE --region us-east-1 \
  --name /redis-adbc-analytics/read-token --with-decryption \
  --query Parameter.Value --output text)"
curl -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $t" \
  https://adbc-telemetry.sa.redislabs.com/api/summary
unset t
# Worker health:
aws cloudwatch describe-alarms --profile YOUR_APPROVED_PROFILE --region us-east-1 \
  --alarm-name-prefix redis-adbc-telemetry-s3 --query 'MetricAlarms[].[AlarmName,StateValue]'
aws logs tail /aws/lambda/redis-adbc-telemetry-s3-refresh \
  --profile YOUR_APPROVED_PROFILE --region us-east-1 --since 1h
```

## Rollback status

The migration plan kept the stopped EC2 host and its encrypted disk for
rollback. Both are gone. CloudTrail shows the host was terminated on 2026-11-19,
about two hours after the migration stopped it and released its Elastic IP. No
detached disk remains. The protected backup bucket remains, and the legacy
security group (no attached interfaces), instance role and profile, and the
approved FinOps lifecycle exception policy still exist. A return to an EC2
collector would need a new host. Its starting data could be:

- the last legacy Redis backup (`backups/2026/11/20/…rdb.gz`, taken
  2026-11-20 01:28 UTC). The 90-day `backups/` rule deletes it around
  2027-02-18, and
- the migration baseline `serverless/baseline.json.gz`, which has no expiry.

Neither contains reports received only by S3 after cutover. Those raw report
objects would have to be reconciled before claiming a complete rollback.

## Retired EC2 collector (2026-11-05 to 2026-11-19)

Historical record. This collector, its host, disk and Elastic IP no longer exist.
Do not run these steps against the live deployment. `restore.py` still applies
to the legacy RDB backups until they expire.

Status (2026-11-05): the collector was recovered after account cleanup automation
terminated the original host. The latest available S3 history was restored, the
existing dashboard read token preserved, and a fresh backup verified. The updated
collector/dashboard includes automatic identity association and the revised privacy
wording. Operational identifiers and recovery records are kept privately.

The user explicitly approved the recovery and a narrow Terraform-managed exception
for the replacement collector. It denies only the approved cleanup principal's
stop/terminate actions on that exact instance; owner administration is unchanged.
Account-required lowercase ownership, service identification and skip-deletion tags
are applied. Tags alone do not establish protection from all cleanup callers.

The driver reports by default to
`https://adbc-telemetry.sa.redislabs.com/v1/usage`. The collector binds to
`127.0.0.1:8787` behind an HTTPS nginx proxy. Redis holds all analytics.

### Resources and access

Terraform in [terraform/](terraform/main.tf) plans these resources in account
`735486936198`, region `us-east-1`:

- One Ubuntu 24.04 EC2 `t3.small` in `subnet-41211f18`, with 30 GiB encrypted gp3
  storage, standard CPU credits (no surplus-credit charges), and a dedicated Elastic IP. IMDSv2 is required; SSH is closed.
- Public TCP 443 for reporting and dashboard access; TCP 80 for ACME certificate
  validation and HTTPS redirects. Redis (6379) and the collector (8787) bind only
  to loopback and have no security-group ingress.
- Route 53 A record `adbc-telemetry.sa.redislabs.com` in zone
  `Z08723911XXA3M3FQCZ4E` (60-second TTL).
- Private bucket `redis-adbc-telemetry-735486936198-us-east-1`: block all public
  access, AES256 encryption, versioning, HTTPS-only policy, 90-day backup expiry.
- Dedicated EC2 role with `AmazonSSMManagedInstanceCore` for administration and
  `s3:PutObject`/`s3:AbortMultipartUpload` restricted to this bucket's `backups/*`.
  No AWS access keys are stored on the server. The server cannot delete backups.

Redis has AOF `everysec`, RDB snapshots, a 512 MiB `noeviction` limit, and separate
collector, backup and admin ACL users. The collector uses pooled RESP3 connections.
Secrets are generated on-host in `/etc/redis-adbc-analytics/collector.env` and
`/etc/redis/users.acl`, outside Terraform state and git. The dashboard read token
is in the environment file; retrieve it privately in an authorized SSM session.

Public ingestion is deliberately unauthenticated for clients. nginx supplies a
private upstream token, limits each IP to 60 requests/minute with a burst of 10,
limits concurrent connections, enforces a 64 KiB body maximum and short timeouts.
The collector validates the payload allowlist and dashboard reads require a
separate token. Reports are unverified product usage feedback, not proof of
customer identity. Access logs are disabled.

This is a single-server deployment with hourly S3 backups, not high availability.
Live Redis history has no automatic expiry; monitor memory and disk capacity.
Base infrastructure is approximately $22/month at 730 hours, before S3 usage
and data transfer. The AWS Price List API returned $0.0208/hour for Linux
`t3.small` in us-east-1 on 2026-11-05. Infrastructure costs continue until resources
are intentionally removed. The
instance and backup bucket have Terraform destruction protection; EC2 also has
termination protection.

### Deploy with Terraform

Keep a private, git-ignored `operator.auto.tfvars` beside the module containing
`source_commit`, the account-required `owner_email`, and (only after explicit
approval) `approved_finops_user`. The exception defaults off; preserve approved
operator settings so a later plan does not inadvertently remove it. Review the
plan before applying. No real account identifiers or operator variables should
be committed as deployment evidence.

The running application uses an immutable, tested commit:

```sh
export AWS_PROFILE=YOUR_APPROVED_PROFILE
terraform -chdir=analytics/deploy/terraform init
terraform -chdir=analytics/deploy/terraform validate
terraform -chdir=analytics/deploy/terraform plan -out=deploy.tfplan
# Review costs, IAM and public ingress before applying the saved plan.
terraform -chdir=analytics/deploy/terraform apply deploy.tfplan
```

State and plans are ignored by git. Terraform state was migrated to the private,
encrypted, versioned S3 bucket at `terraform/terraform.tfstate`, with native S3
state locking (`use_lockfile=true`, Terraform >=1.10). The instance role can only
write `backups/*` and cannot access the Terraform state. The lifecycle expiration
rule only applies to backups. Keep this backend bucket for the lifetime of the
deployment. The bucket was created during the initial local-state apply, before
the backend migration; new operators should initialize against the existing
backend. Never apply a plan whose source commit predates the bootstrap script.

Allow cloud-init several minutes to install official Redis packages and Python
dependencies and obtain the TLS certificate. Terraform completion alone does not
prove the service is ready. Check:

```sh
curl --fail https://adbc-telemetry.sa.redislabs.com/health
# Expect 401 without the dashboard token:
curl -o /dev/null -w '%{http_code}\n' https://adbc-telemetry.sa.redislabs.com/api/summary
aws ssm start-session --region us-east-1 --target INSTANCE_ID
# In that private session:
sudo cloud-init status --long
sudo systemctl status redis-adbc-analytics redis-adbc-backup.timer
sudo journalctl -u redis-adbc-backup.service --no-pager
```

Bootstrap source is pinned by the Terraform `source_commit` variable. Updating
that variable does not rerun cloud-init on an existing machine; use SSM to stage
and verify a new application release, with a backup first. Do not replace the
Redis instance without a verified restore plan.

### S3 backup and restore

`redis-adbc-backup.timer` exports a fresh consistent RDB every hour using the
backup ACL user. `redis-check-rdb` must pass before gzip and upload. Each object
has an S3 SHA256 checksum, AES256 encryption and a JSON manifest containing the
uncompressed SHA256, size and source revision. Failed exports/uploads fail the
systemd unit; no success is logged before both objects are uploaded. Backup
freshness must be monitored operationally. First bootstrap runs a backup too.

```sh
sudo systemctl start redis-adbc-backup.service
aws s3 ls s3://redis-adbc-telemetry-735486936198-us-east-1/backups/ --recursive
# Run as an operator with S3 read access (the host role only writes backups):
python3 analytics/restore.py \
  --bucket redis-adbc-telemetry-735486936198-us-east-1 \
  --key backups/YYYY/MM/DD/TIMESTAMP.rdb.gz --output /private/tmp/restored.rdb
```

Restore verifies the manifest, decompressed size, SHA256 and `redis-check-rdb`
before creating a new mode-0600 output file. It refuses an existing destination.
Test the RDB in an isolated Redis instance first. For disaster recovery, stop the
collector and Redis, preserve the existing data directory, stage the verified RDB
as Redis's `dump.rdb` with correct ownership, and start Redis without the old AOF
files (otherwise Redis prefers the AOF). Then verify summary totals before
restarting ingestion. Restore in the isolated instance with AOF disabled first;
re-enable AOF after verifying data. Never restore over a running database.

References: [official Redis packages](https://redis.io/docs/latest/operate/oss_and_stack/install/install-stack/apt/),
[Redis persistence and RDB backups](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/),
[SSM instance permissions](https://docs.aws.amazon.com/systems-manager/latest/userguide/setup-instance-permissions.html).


Deployment evidence is in
[aws-deployment.json](../../docs/sdlc/changes/adoption-analytics/evidence/aws-deployment.json).
Repository CI/tests opt out of hosted telemetry. Initial validation traffic before
that opt-out may appear as unattributed usage; specifically labeled deployment
smoke records were removed after backup/restore verification. Public data remains
self-reported and should not be treated as verified customer adoption.


Application updates can run `analytics/deploy/update.sh FULL_COMMIT_SHA` as root
through SSM. The script takes a backup before updating, preserves existing tokens,
stages files and dependencies, updates the proxy source header/local Redis ACLs,
and retains application rollback files. Source DNS collection uses the overwritten
nginx `X-Real-IP` header and explicit `ANALYTICS_TRUST_PROXY=1`; never trust a
client-supplied forwarding header from a non-loopback peer. Reverse DNS is cached
and resolved asynchronously. No AWS resources or IAM permissions are added for
these dashboard changes.


Automatic association adds a Redis alias hash to the existing persisted dataset;
RDB/S3 backups include it automatically. The collector ACL gains only `HGET`
within its existing analytics key prefix. No new infrastructure, IAM permissions,
user UUID configuration or mounted ID volume is required. The deployment map is
not an authenticated customer directory: reports remain self-reported and shared
network evidence never performs identity merges.


Recovery retains the existing static endpoint and encrypted/versioned state.
Verify the exact-instance IAM exception by reading it back and simulating the
known cleanup principal and owner separately. Do not assume API termination
protection or generic ownership tags prevent privileged account automation from
terminating the host. A different cleanup caller is outside the narrow exception.

No manual UUID or ID-file volume is required for automatic deployment association.
Provider discovery needs an updated driver build; the collector also associates
incoming legacy reports by installation ID and shows network-only suggestions
without merging deployment identities.
