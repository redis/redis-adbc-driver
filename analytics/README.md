# Driver adoption analytics

We collect high-level usage metrics—such as statement types, execution counts,
timings and row counts—to understand how the driver is used and help improve it.
**Your SQL statements, query parameters, query results and database contents are
not collected.** Source hostnames, network observations and pseudonymous deployment
identifiers provide context for these metrics; their handling is described below.

Collection is on by default. Set `adbc.redis.telemetry.enabled` to `false` before
opening a connection to opt out of both collection and reporting.

The default reporting endpoint is the AWS collector at
`https://adbc-telemetry.sa.redislabs.com/v1/usage`, under the `sa.redislabs.com`
Route 53 zone. Reporting does not require client configuration or a shared
secret. Python, SQLAlchemy, Superset, Dagster and DuckDB share these options when
their integration forwards them; the caller supplies the integration label.
Setting `endpoint` to an empty string keeps collection local without network
reporting. `REDIS_ADBC_TELEMETRY_DISABLED=true` changes the default to disabled;
an explicit database `enabled` option takes precedence.

## Run the collector and dashboard

Requires Python 3.10+, the dependencies in `requirements.txt`, and a dedicated
Redis database. Redis holds all analytics; no SQLite or RDS database is used.

```sh
python3 -m pip install -r analytics/requirements.txt
export ANALYTICS_REDIS_URL='redis://127.0.0.1:6379/0'
# Generate two different secrets; do not commit them.
export ANALYTICS_WRITE_TOKEN="$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')"
export ANALYTICS_READ_TOKEN="$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')"
python3 analytics/collector.py
```

Open <http://127.0.0.1:8787/> and enter the read token. The collector binds to
loopback. `/api/summary` requires the read token; the dashboard shell contains no
usage data and does not persist the token. `/health` checks Redis availability.
Self-hosted clients use the write token. The AWS HTTPS proxy injects its private
write token on public ingestion requests, validates body limits and rate limits
per IP; clients do not contain a secret. Public reports and labels remain
unverified and can be spoofed. Tokens are generated on the host, outside Terraform
state. Access logs are disabled.

Redis uses pooled RESP3 connections, per-session WATCH/MULTI retry deduplication,
a report stream, bounded read pages and indexed download snapshots. The deployment
uses append-only persistence, `noeviction`, and hourly validated RDB exports to a
private, encrypted, versioned S3 bucket with 90-day backup retention. Live Redis
data has no automatic expiry; size the instance as usage grows. The deployment is
a single server, so it is not highly available. Disk loss can lose up to an hour
of writes since the last successful S3 backup.

See [Terraform deployment and restore](deploy/README.md) for infrastructure,
credentials, backup checks and restore instructions. Deployment status is recorded
there separately from the configuration.

## Configure reporting or opt out

Build a new native library (`make -C go build`) or install a wheel containing
this feature. Existing released wheels will not acquire these options until a
new release. New clients create a random installation ID once in the local user configuration
directory and reuse it across database handles and process restarts. Users do not need to mount an ID file or configure a UUID: deployment association
automatically uses stable platform metadata where available. Local installation
IDs can change when containers are replaced; they remain separate from the
collector-assigned deployment ID.
Do not use a hostname, email address or Redis URI as the ID. Read-only/unusable
configuration storage falls back to a temporary ID and marks it as temporary.

```python
import os
from adbc_driver_redis import dbapi

with dbapi.connect(
    os.environ["REDIS_URI"],
    db_kwargs={
        "adbc.redis.telemetry.installation_id": "c2f2a874-fc8f-4214-aada-d265d8fc7233",
        "adbc.redis.telemetry.organization": "Example company",
        "adbc.redis.telemetry.team": "Finance analytics",
        "adbc.redis.telemetry.application": "Revenue dashboard",
        "adbc.redis.telemetry.environment": "production",
        "adbc.redis.telemetry.integration": "python-dbapi",
        "adbc.redis.telemetry.use_case": "interactive-bi",
    },
) as connection:
    with connection.cursor() as cursor:
        cursor.execute("SELECT 1")
        assert cursor.fetchall() == [(1,)]
```

Organization/team labels are **self-reported**, not verified company identities.
You can map organization IDs to your authenticated customer directory separately.
Unlabeled installations remain unattributed. For IP-based inference, the existing
README Scarf pixel is a separate documentation signal; this collector does not
infer company identity from IPs or import Scarf account data.

Configuration options below are database options and must be set before the first Open.
The read-only snapshot and explicit flush operation are available after Open.
They are exported as Go constants in `usage.go` and Python `DatabaseOptions`.

| Option suffix after `adbc.redis.telemetry.` | Default | Meaning |
|---|---|---|
| `enabled` | `true` | Set `false` to opt out of collection and reporting |
| `endpoint` | `https://adbc-telemetry.sa.redislabs.com/v1/usage` | AWS collector; explicitly empty means local snapshots only |
| `token` | empty | Write bearer token; never returned by GetOption or included in reports |
| `installation_id` | generated and saved locally | Explicit application/deployment ID overrides the generated ID |
| `installation_id_file` | user configuration directory + `redis-adbc-driver/installation_id` | Shared file used for generated IDs; an empty/unusable path falls back to a temporary ID |
| `source_hostname` | operating-system hostname | Self-reported source machine name; override with a DNS name or set empty to omit |
| `organization`, `team`, `application`, `environment`, `use_case`, `integration` | empty | Optional labels, each at most 256 UTF-8 bytes |
| `interval` | `60s` | Report interval; between `1s` and `24h` |
| `snapshot` | `{}` when disabled | Read-only JSON cumulative snapshot, readable from database or connection |
| `flush` | write-only | Set to `true` to synchronously deliver one snapshot (two-second HTTP timeout); use before exiting |

Non-loopback endpoints require HTTPS. URLs cannot contain user info, query strings
or fragments. Redirects are refused so tokens are not forwarded elsewhere.
Collection and reporting to AWS are enabled when options are omitted. A custom
endpoint can redirect reports to your own collector. To collect and send nothing:

```python
with dbapi.connect(
    os.environ["REDIS_URI"],
    db_kwargs={"adbc.redis.telemetry.enabled": "false"},
) as connection:
    pass
```

Label values are included verbatim: use organization identifiers
and workload descriptions suitable for sharing, without secrets.

Short-lived low-level clients can force delivery after draining and releasing results:

```python
# database is an adbc_driver_redis.connect(...) low-level ADBC database.
# This is an explicit network operation and raises on delivery failure.
database.set_options(**{"adbc.redis.telemetry.flush": "true"})
```

## Metric definitions and limits

- An **attempt** is one `ExecuteQuery` or `ExecuteUpdate` call. A SQL script or
  parameter batch is one attempt, not one per embedded statement, parameter row,
  Redis command, retry or result batch. Scripts have type `script`. Invalid SQL
  has type `unknown`. Bulk ingest has type `ingest`.
- Query results succeed only when the reader reaches exhaustion. A later read
  error or cancellation is not success. Final reader release before exhaustion
  is `abandoned`. Even buffered SELECT results follow this consumption contract.
  `ExecuteUpdate` and bulk ingestion finish when their API call returns.
- Rows count batches actually delivered to the application. Affected rows are
  the nonnegative count reported by update/ingestion APIs; partial writes on a
  failed operation are not estimated. Byte volume is not currently collected.
- Latency is measured with a monotonic clock. End-to-end latency includes caller
  fetch time; ready latency measures `ExecuteQuery` returning its reader.
  Histograms have disjoint buckets and an overflow bucket. Dashboard p50/p95/p99
  values are bucket upper bounds, not exact percentiles or Redis execution time.
- SELECT feature flags cover the top-level SELECT's clauses and expressions.
  Nested bodies, view expansion and physical pushdown/fallback plans are not
  classified. Metadata reflects SQL SHOW calls; ADBC GetObjects/GetInfo and other
  metadata APIs are outside the execution counters.
- Reports contain fixed query kinds/feature names, counts, histograms, driver
  version, OS/architecture, timestamps, random IDs, ID storage status, the source
  hostname and explicit labels. Recent per-execution samples contain only fixed
  classifications/features, anonymous execution IDs, times and numeric work counts.
  They contain no SQL, literals, query hashes, parameter values, schema/table/
  column names, result values, Redis addresses, credentials or error messages.
- One reporter runs per database handle, shared by its connections. Reports are
  cumulative with a fresh random session ID per handle. The collector ignores
  duplicate/older sequences and differences later snapshots, recovering missed
  reports without double counting. Installation IDs identify deployments, not
  people, and cannot deduplicate downloads.
- HTTP runs in a background goroutine with a two-second timeout and bounded
  in-memory state. Query/connection execution does not wait for reporting.
  Closing a database requests one final asynchronous report; immediate process
  exit can lose it. There is no durable client spool. For local/offline or
  short-lived applications, retrieve `snapshot` and persist/send it yourself
  before exiting, or explicitly flush the low-level database before exiting. Collection still
  works with an unreachable endpoint; `delivery_failures` records failed sends.
- Dashboard time windows use **UTC collector receipt time**, not precise query
  execution dates. Delayed reports can shift activity into later days. Attempts
  and completions can fall into different windows. Error rate is errors divided
  by all terminal outcomes (success, error, canceled, abandoned). Active installations have
  attempts in the relevant window. Week-4 retention uses successful executions
  on days 28–35 after first observed success, with fully elapsed cohorts only.
  Older clients generated IDs per handle; temporary IDs and recreated container
  files can still inflate counts. Weekly active installations are distinct IDs
  with an attempt received during the last seven days, not unique machines/people.
- Data covers participating, reachable reporting installations only. Unreported
  users and installations are unknown. Labels alone do not verify production use.

## Automatic identity generation and association

No UUID configuration or mounted ID file is required:

- **Runtime instance ID:** a generated UUID shared across database handles in one
  driver process; a new runtime gets a new ID. This is not a count of people.
- **Installation ID:** automatically generated and saved locally when possible,
  preserving the existing explicit-ID override. Ephemeral containers may get new
  installation IDs. No persistent volume is needed for platform-based association.
- **Deployment ID:** generated by the collector and kept durably in Redis/S3.
  Matching stable workload hashes map replacement containers to the same ID,
  independent of installation UUID and public IP changes.

Automatic discovery currently supports ECS services and Kubernetes workloads:

- ECS reads the local v4 task metadata endpoint and hashes the account/region,
  cluster and service identity, excluding task IDs and revision numbers. It needs
  a service name; task family or cluster alone never merges unrelated services.
- Kubernetes uses the pod's existing service-account access and verified API TLS
  to follow ReplicaSet → Deployment or Job → CronJob ownership, or a StatefulSet/
  DaemonSet controller UID. The controller UID survives pod/ReplicaSet replacement.
  No permissions are created or elevated. Missing mounts, denied RBAC, unsupported
  owners or unavailable metadata automatically fall back without breaking queries.
- Metadata discovery runs once per process in the background, with a two-second
  total deadline. It never blocks SQL execution. The first HTTP delivery/explicit
  flush can wait for it. Redirects and proxy forwarding of metadata are disabled.
  Only a provider name and SHA256 workload key reach the collector—not raw platform
  names, ARNs, UIDs, service-account tokens, credentials or metadata responses.

When a platform key is unavailable, the collector automatically associates reports
by the generated installation ID. When local ID storage is unavailable, the
automatic runtime ID also associates multiple handles despite changing temporary
installation UUIDs. A changed IP/PTR does not split those associations.
If both UUID and stable platform identity are unavailable/changed, shared IP/PTR
can create a **probable network-only association**. It never merges deployment IDs
or reduces their distinct count. Shared NAT/VPNs and recycled addresses make these
suggestions uncertain. There is no reliable universal device/deployment identity
available to arbitrary fresh containers without a stable signal.

The dashboard distinguishes active installation IDs, associated active deployments,
runtime instances, legacy database handles and probable network suggestions.
Identifier matches are **self-reported**, not authenticated company identities.
Historical reports without association metadata remain outside deployment counts;
new incoming legacy reports still receive automatic installation-based association.

Discovery references: [ECS v4 task metadata](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-metadata-endpoint-v4-response.html),
[Kubernetes API access from pods](https://kubernetes.io/docs/tasks/run-application/access-api-from-pod/).

## Reporting sources and per-execution workloads

The dashboard lists both the client-reported hostname and reverse DNS (PTR) of the
public egress IP observed by the HTTPS proxy. A hostname is self-reported; a PTR
name may identify a NAT/VPN/shared gateway rather than the machine. The proxy
replaces `X-Real-IP`; the collector trusts it only from a loopback peer when
`ANALYTICS_TRUST_PROXY=1`. Direct clients cannot supply an observed network name
in their JSON. Older stored reports cannot be retroactively attributed.

PTR lookups run in two background workers, with two-second DNS deadlines, a
bounded queue and one-hour cache. They do not block ingestion. Failed/missing
lookups display as unavailable; reload to see completed lookups. The source list
shows saved-local, application-supplied, temporary or unknown ID status.

Recent workload samples show one ADBC execution at a time:

- **Index documents observed**: sum of distinct index/shard sizes already read by
  existing readiness checks. This is point-in-time index population, which can
  cover more than one logical table. No extra metadata commands or scans run.
- **Driver row candidates**: candidates fetched for driver-side evaluation,
  including repeated scans, joins and vanished-row slots. This does not measure
  Redis internal index work or prove every table row was visited.
- **Pushdown rows represented**: rows represented by supported aggregate tuples,
  including retry work; separate from physical driver fetches.
- **Rows returned/changed**: delivered result rows and known affected counts for
  that execution. Missing measurements remain `null`. The dashboard distinguishes “Not applicable”
  for known inapplicable counters from “Unavailable” for unreported counters, with
  per-execution notes and tooltips. Explicit zero values remain zero. Missing
  reports cannot prove which internal execution path caused a missing counter.
- **Outcome and elapsed time**: terminal result and time through consumption or
  release; no success before reader exhaustion.

The client keeps its latest 24 completed execution samples; reporting retries
resend them safely, deduplicated by session/execution ID. Busy clients can rotate
samples out before a report, so this is not a complete query log. Redis retains
10,000 recent samples and the dashboard shows up to 200. Aggregate reliability
counters remain cumulative. No raw SQL, plans, table names or query fingerprints
are transmitted. Updated drivers are required for hostname, saved-ID status and
workload samples; the collector remains compatible with older aggregate reports.

## Download snapshots

```sh
# Optional GITHUB_TOKEN increases the API rate limit; never stored in Redis.
python3 analytics/downloads.py \
  --repo redis/redis-adbc-driver --package adbc-driver-redis
```

Run once daily using your scheduler. This script saves paginated GitHub release
asset lifetime counters (including per-asset/version data) and PyPI Stats recent
1/7/30-day counts in the same Redis database. Each channel is fetched and saved
independently; partial failures exit nonzero. The dashboard shows recent snapshots.
GitHub counters include build assets, checksums and possible packaging-job traffic;
these are not unique installs. Do not add rolling PyPI counts across snapshots or
sum channels and label the result unique users. Version/platform PyPI analysis
requires the public BigQuery dataset; it is not inferred from these totals.

References: [PyPI download dataset](https://packaging.python.org/en/latest/guides/analyzing-pypi-package-downloads/),
[PyPI Stats API](https://pypistats.org/api/),
[GitHub releases API](https://docs.github.com/en/rest/releases/releases),
[Scarf matching](https://docs.scarf.sh/company-matching/).

## Checks

```sh
ANALYTICS_TEST_REDIS_URL="$ANALYTICS_REDIS_URL" python3 -m unittest discover -s analytics -p 'test_*.py' -v
cd go && env -u REDIS_URI REDIS_ADBC_TELEMETRY_DISABLED=true go test -race -count=1 -p 1 -run '^TestUsage' ./...
```

These checks cover wire validation, access control, retry/order handling,
retention, pagination, Arrow reader ownership, cancellation, late errors,
privacy filtering and concurrent snapshots. The normal Go suite remains the
regression check for existing driver behavior. Repository CI and Go TestMain
opt out of hosted reporting; the explicit hosted smoke test requires
`REDIS_ADBC_VERIFY_HOSTED_TELEMETRY=1`. Redis-backed tests need a
dedicated Redis Software database: collector tests use `ANALYTICS_TEST_REDIS_URL`,
and Go integration tests use `REDIS_URI`. Unconfigured integration tests skip.


Dashboard table controls (2026-11-05): each table has search across loaded columns,
case-insensitive substring filters per column (combined with AND), ascending/descending
column sorting, page sizes of 10/25/50/100 and Previous/Next navigation. Numeric
counts sort numerically, dates chronologically, and missing measurements last.
Reset table clears filters/sort and restores 25 rows. Controls persist across
refreshes in the current page. Pagination does not fetch additional history: the
query endpoint still supplies at most 200 recent samples.
