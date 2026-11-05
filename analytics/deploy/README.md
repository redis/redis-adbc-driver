# AWS Redis analytics deployment

Status (2026-11-05): deployed through Terraform after explicit approval.
No RDS resources are included or were created.

- Collector: `i-0a0f07f12f13d4b52` (`100.29.239.174`).
- Application source: `be6c31ed026e9f915615fa4da29c5dfc21e620b0`.
- DNS/TLS and the health endpoint passed; collector, Redis and nginx are active.
- Backup and TLS-renewal timers are enabled. A real S3 RDB backup was downloaded,
  checksum-verified, checked with `redis-check-rdb`, and loaded in an isolated Redis
  process with the synthetic reports present.
- Project, purpose, Terraform management and ownership tags are applied, including
  the root volume. Descriptive `Name` tags are managed through Terraform.

The driver reports by default to
`https://adbc-telemetry.sa.redislabs.com/v1/usage`. The collector binds to
`127.0.0.1:8787` behind an HTTPS nginx proxy. Redis holds all analytics.

## Resources and access

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

## Deploy with Terraform

The deployed application uses the pinned, tested commit below:

```sh
export AWS_PROFILE=735486936198_SuperUserAccess
terraform -chdir=analytics/deploy/terraform init
terraform -chdir=analytics/deploy/terraform validate
terraform -chdir=analytics/deploy/terraform plan \
  -var="source_commit=be6c31ed026e9f915615fa4da29c5dfc21e620b0" -out=deploy.tfplan
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

## S3 backup and restore

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
