# Driver adoption analytics

The driver can collect aggregate usage in its shared Go layer. Reporting is
**off by default**. There is no built-in vendor endpoint. Python, SQLAlchemy,
Superset, Dagster and DuckDB can use the same database options when their
integration forwards them; the caller supplies the integration label.

## Run the collector and dashboard

Requires Python 3.10+ and no third-party packages. From the repository root:

```sh
# Generate two different secrets and store them in your deployment's secret store.
export ANALYTICS_WRITE_TOKEN="$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')"
export ANALYTICS_READ_TOKEN="$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')"
python3 analytics/collector.py --db /private/tmp/redis-adbc-analytics.sqlite3
```

Open <http://127.0.0.1:8787/> and enter the read token. Give reporting clients
only the write token. The read token protects `/api/summary`; the dashboard shell
contains no usage data and does not persist the token. `/health` is public.

This is a small self-hosted collector, using SQLite WAL and Python's HTTP server.
For a shared deployment, run behind an HTTPS reverse proxy with request timeouts,
rate limits and restricted dashboard access; bind the backend to loopback.
Back up the SQLite database, including WAL state using SQLite's backup tooling.
The server does not log request addresses or tokens. TLS termination and hosting
are deployment steps; nothing is provisioned or published by this change.

## Enable a reporting application

Build a new native library (`make -C go build`) or install a wheel containing
this feature. Existing released wheels will not acquire these options until a
new release. Use a stable, randomly generated installation ID per deployment;
persist it in that deployment's configuration. Do not use a machine hostname,
email address or Redis URI as the ID.

```python
import os
from adbc_driver_redis import dbapi

with dbapi.connect(
    os.environ["REDIS_URI"],
    db_kwargs={
        "adbc.redis.telemetry.enabled": "true",
        "adbc.redis.telemetry.endpoint": "http://127.0.0.1:8787/v1/usage",
        "adbc.redis.telemetry.token": os.environ["ANALYTICS_WRITE_TOKEN"],
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
| `enabled` | `false` | Explicit `true` enables collection |
| `endpoint` | empty | POST destination; empty means local snapshots only |
| `token` | empty | Write bearer token; never returned by GetOption or included in reports |
| `installation_id` | random per database handle | Supply a persisted random ID for retention/deduplication across restarts |
| `organization`, `team`, `application`, `environment`, `use_case`, `integration` | empty | Optional labels, each at most 256 UTF-8 bytes |
| `interval` | `60s` | Report interval; between `1s` and `24h` |
| `snapshot` | `{}` when disabled | Read-only JSON cumulative snapshot, readable from database or connection |
| `flush` | write-only | Set to `true` to synchronously deliver one snapshot (two-second HTTP timeout); use before exiting |

Non-loopback endpoints require HTTPS. URLs cannot contain user info, query strings
or fragments. Redirects are refused so tokens are not forwarded elsewhere.
Configuration alone does not enable reporting. Set `enabled=false` or omit it to
collect nothing. Label values are included verbatim: use organization identifiers
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
  version, OS/architecture, timestamps, random IDs and the explicit labels.
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
  Anonymous IDs generated on every restart cannot measure cross-restart retention.
- Data covers participating, reachable reporting installations only. Unreported
  users and installations are unknown. Labels alone do not verify production use.

## Download snapshots

```sh
# Optional GITHUB_TOKEN increases the API rate limit; never stored in SQLite.
python3 analytics/downloads.py --db /private/tmp/redis-adbc-analytics.sqlite3 \
  --repo redis/redis-adbc-driver --package adbc-driver-redis
```

Run once daily using your scheduler. This script saves paginated GitHub release
asset lifetime counters (including per-asset/version data) and PyPI Stats recent
1/7/30-day counts in the same SQLite database. Each channel is fetched and saved
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
python3 -m unittest discover -s analytics -p 'test_*.py' -v
cd go && env -u REDIS_URI go test -race -count=1 -p 1 -run '^TestUsage' ./...
```

These checks cover wire validation, access control, retry/order handling,
retention, pagination, Arrow reader ownership, cancellation, late errors,
privacy filtering and concurrent snapshots. The normal Go suite remains the
regression check for existing driver behavior. Redis-backed tests need a
dedicated Redis Software database and are skipped without `REDIS_URI`.
