# ClickBench on Redis

Run the official ClickBench PostgreSQL query variant through Redis ADBC on a
colocated Redis Cloud or verified self-hosted deployment. The harness loads all 105 columns, isolates
queries in separate processes, records failures, and validates outputs against
DuckDB references before reporting latency ratios.

See [loading-improvements.md](loading-improvements.md) for the loading changes,
local pilot measurements and validation. Operational deployment records and
live benchmark artifacts are kept outside the reusable harness.

## Deployment and placement

New Cloud benchmark deployments default to **Redis Cloud Pro**. Use a dedicated
non-production database and record the cost estimate, owner and cleanup date.

Run the benchmark on a cloud VM in the database's **provider, region, and
physical availability zone**, as requested. Verify the Redis primary shard
placement from the Redis Cloud control plane and the VM placement from the
cloud provider API. Across AWS accounts use stable AZ IDs, not account-specific
AZ names. Record which primary placement is being matched if the database has
replicas. A database whose primary shards span AZs cannot share one AZ with a
single runner; resolve that deployment shape before benchmarking.

Create `placement.json` on the runner using verified values and evidence
references; do not store authentication credentials in it:

```json
{
  "database": {
    "provider": "aws",
    "region": "us-east-1",
    "az": "use1-az1",
    "evidence": "Redis Cloud database ID and control-plane placement readback"
  },
  "runner": {
    "provider": "aws",
    "region": "us-east-1",
    "az": "use1-az1",
    "evidence": "EC2 instance ID and DescribeInstances AZ-ID readback"
  }
}
```

These are illustrative values, not a selected or verified deployment. The
runner rejects missing/mismatched placement fields. The file records external
verification; the script itself cannot independently attest infrastructure
placement. Include instance shape, RAM, shards, QPF, replication, TLS, and
network path in `--machine` and save the control-plane configuration with the
results. Use a dedicated benchmark database with Search and RAM storage.

## Run on the deployed host

Install Go, a C compiler, Python and uv on the VM and copy this source tree
there. Build natively for its OS and architecture; do not transfer the macOS
library to Linux. Download data before starting the ingestion timer.

Query trials require Linux with systemd, cgroup v2 memory control, and permission
to create transient services in the system manager (run the dedicated runner as
root). The harness checks this before loading data and fails if isolation cannot
be enforced. Each query runs in its own sibling service, so a trial OOM cannot
kill the benchmark supervisor through its cgroup. The kernel caps the whole
trial, including the Go driver, Arrow buffers, Python output and descendants.

The default limit is half of available host/cgroup RAM, leaving room for the OS
and supervisor. Override with `--trial-memory-mib 61440` for a 60 GiB cap;
overrides must leave at least 25% of available RAM free. Swap is disabled for
trials. Results record the cap and, when systemd retains it, the memory peak.
Exceeding the cap records `MemoryLimit` and a null timing, then the sweep
continues. Timeouts stop the entire trial service, including descendants.
Credentials are passed through stdin rather than service command arguments.

These limits protect the runner; they do not implement disk spilling or make a
high-cardinality query succeed with less RAM. Redis's server-side aggregate group
limit is independent. Supported simple aggregate queries now use
[partitioned exact aggregation](../../docs/partitioned-exact-aggregates.md),
with disk spilling and recovery from that group limit. Unsupported plans may
still return the server error. The driver outside this benchmark harness does
not acquire a hard per-query process memory limit.

```sh
make -C go build
git clone https://github.com/ClickHouse/ClickBench.git /tmp/ClickBench
curl --fail --location --output /tmp/hits.parquet \
  https://datasets.clickhouse.com/hits_compatible/hits.parquet
```

Record/pin the checkout commit for reproducibility. The reference report uses
`e90045879276f75cc398f1ab8a73541a7716058d`. Official source and dataset licenses
apply separately; they are not vendored into this repository.

Inject the authenticated URI as `REDIS_CLOUD_URI` through the cloud secret
manager or a protected environment. Use the private endpoint when available
and `rediss://` when TLS is enabled. The runner accepts Redis Cloud hostnames
ending in `.redis-cloud.com`, `.redislabs.com`, `.db.redis.io`, or `.cloud.rlrcp.com` and has no localhost fallback.
For a verified custom Cloud hostname, update that check before executing.

Pilot (100,000 official rows, all 105 columns and 43 queries):

```sh
uv run --no-project --with pyarrow --with redis --with adbc-driver-manager \
  python benchmarks/clickbench/run.py \
  --upstream /tmp/ClickBench --parquet /tmp/hits.parquet \
  --placement placement.json --machine '<verified database and runner shape>' \
  --rows 100000 --output pilot.json --cleanup
```

Full scale, after checking the pilot's actual memory/compute requirements:

```sh
uv run --no-project --with pyarrow --with redis --with adbc-driver-manager \
  python benchmarks/clickbench/run.py \
  --upstream /tmp/ClickBench --parquet /tmp/hits.parquet \
  --placement placement.json --machine '<verified database and runner shape>' \
  --rows 99997497 --output full.json
```

Loading uses one synchronous writer by default. Add `--load-prefetch` to overlap
source preparation with one writer, or use independent append connections with
`--load-workers 4` or
`--load-workers 8`. At most two batches per worker are queued; all workers finish
before index/count verification and query timing. More workers can hurt a
single-shard server or an indexing-bound workload, so measure before increasing
the count. The driver retains its 1,000-command pipeline limit.

Parallel loads allocate unique row IDs in execution order. Their row IDs no
longer identify source offsets, so `--resume-result` rejects parallel runs.
Interrupted parallel loads require a fresh generated schema. Results record the
worker count, batch size, prefetch bound, and driver binary SHA-256.

`load_profile.py` compares the old serial loader to optimized 1/4/8-worker
loading using the same official source prefix and all 105 columns. It checks
source-derived aggregates and 100 complete rows against DuckDB after indexing.
Use an isolated Redis server, set `REDIS_URI`, and supply `--baseline-driver`,
`--driver`, `--upstream`, `--parquet`, and a fresh `--output`. These pilot results
are loading measurements, not official full-scale ClickBench leaderboard entries.

Every run creates a fresh `clickbench_<random>` schema, never replaces an
existing table, and retains it unless `--cleanup` is passed. Cleanup drops
only that run's table and schema. Results are written incrementally and are
never overwritten. Load time includes DDL, Parquet decoding, Arrow ingestion,
index readiness and count verification; file download is excluded. Queries
use the official PostgreSQL variant unchanged, with default exact aggregates.
The first run is not a true cold run. Failed trials are null; no extra warm-up
is added. Fingerprint calculation is outside the query timing.

## Compare and verify

### Self-hosted clusters

Select `--deployment self-hosted` explicitly and set `REDIS_URI` to a loopback
seed or a private IP explicitly recorded in verified `placement.json` under
`database.primary_hosts`. The dedicated runner may also host one Redis primary. The driver
and the Python control client discover Redis Cluster from that seed. All cluster
addresses must be reachable privately. Index readiness checks the global document count reported by every primary
and waits until every primary reports `indexing=0`. Redis Search coordinates
these replies; adding their counts would count the same documents repeatedly. Interrupted
cluster loads cannot use `--resume-result`.

Provide the same verified placement file and a finite `--deadline` in UTC epoch
seconds. Record all instance IDs, primary/replica counts, total CPU/RAM, server
memory limits, client placement and container image digests with the result.
For example, six `c6a.4xlarge` primaries have 96 vCPUs and 192 GiB of host RAM
in total; this allocation is not equivalent to a single `c6a.4xlarge` baseline.
Full AWS runs retain a mandatory live FinOps preflight. The existing Cloud IAM
deny guard remains available. The self-hosted alternative in `cluster_finops.py`
verifies every exact instance ID, required owner/run/expiry tags, API termination
protection, tagged root volumes with DeleteOnTermination, and the security group.
It also verifies the pinned cleanup Lambda code, role, immutable run configuration,
enabled five-minute schedule and role permissions to remove protection and terminate
every owned node at the deadline. This alternative is EC2 attribute protection;
it is not an IAM deny and a caller with attribute-modification permission can remove it.
Use a protected `--finops-config` and, when needed, `--aws-preflight-stdin` for
short-lived operator credentials; never put credentials in the configuration,
command arguments or benchmark artifacts. Start with a
source-prefix pilot to measure indexed storage and client memory requirements;
pilot timings must not be ranked against full-data leaderboard entries.

The default Cloud mode continues to require `REDIS_CLOUD_URI` and an allowed
Cloud hostname; self-hosted mode does not silently change that default.

### Compare statement and public partition APIs

The 43-query ClickBench run calls ordinary `cursor.execute()` and fully fetches
its Arrow output. The driver may parallelize internally. The public
`adbc_execute_partitions()` / `adbc_read_partition()` API supports row-local
scans, filters and projections; it rejects global aggregation, ordering and
limits. Internal `partitioned_exact` execution is a separate feature.

`scan_paths.py` compares both public APIs against the existing, fully loaded
benchmark namespace without creating, loading or deleting tables:

```sh
python benchmarks/clickbench/scan_paths.py \
  --prior-run /opt/clickbench/results/full.json \
  --driver '<same driver library recorded in full.json>' \
  --parquet /opt/clickbench/hits.parquet --upstream /opt/clickbench/upstream \
  --output /opt/clickbench/results/scan-paths.json \
  --data-directory /opt/clickbench/scan-data \
  --deadline '<finite UTC epoch deadline leaving time for backup>'
```

Run this on the same verified deployment after the main sweep and validation.
It enforces the recorded namespace, endpoint and driver library hash, uses
16 GiB sibling service limits and compares three trials per API for three scan
cases: a narrow full-table projection, a full-table residual filter, and a
one-million-row-ID mixed-type projection. API order alternates between paired
trials. Public partition reads use up to 16 external processes and 16 descriptors;
statement execution has a 16-worker driver budget on the same client.

Both timers include connections, complete consumption and Arrow IPC writes;
the partition timer also includes planning and worker startup/teardown. Exact
multiset/schema validation runs outside timing, preserving duplicates and
ignoring result order. Full/filtered cases additionally check every returned
row against the typed Parquet source. The bounded case checks dense row-ID
coverage and full cross-API equality, with 100 independent source-membership
samples; parallel-load row IDs do not identify source offsets. Failed or
unverified timings are null, and medians require three valid paired trials.
Raw Arrow outputs remain on disk for backup and independent inspection.
This supplemental scan workload must not be presented as another full ClickBench
result or ranked against its published query timings.

The runner checks row count, records result fingerprints and captures failure
types/statuses. It does **not** independently validate query values. Validate
them against another engine using the same source rows, handling unordered
results and ties correctly, before publishing a ranking. Failed queries must
remain visible. Database storage including indexes still needs a measured
value: `data_size` is intentionally null, rather than mislabeling whole-server
memory as table storage. Concurrent-QPS tests in the current upstream are not
implemented by this runner yet.

For a full-scale result, print provisional per-query ratios against the
published entries in the report:

```sh
uv run --no-project --with pyarrow --with redis --with adbc-driver-manager \
  python benchmarks/clickbench/compare.py full.json \
  /tmp/ClickBench/clickhouse-cloud/results/20261008/aws.3.32.json \
  /tmp/ClickBench/clickhouse/results/20261008/c6a.4xlarge.json \
  /tmp/ClickBench/duckdb/results/20260511/c6a.4xlarge.json \
  /tmp/ClickBench/postgresql/results/20260921/c6a.4xlarge.json
```

Pilot/full-scale numerical comparisons are rejected. Hot time is the smaller
of runs two and three only when both succeed and independently validate against the reference. Unverified timings remain visible without ratios. The ratio is Redis latency
divided by the baseline latency, without assigning an overall rank. The
ClickBench dashboard uses a different shifted geometric-mean score, including
failure penalties; these direct latency ratios are not that score.

Helper checks (no database access or benchmark timings):

```sh
uv run --no-project --with pyarrow --with redis --with adbc-driver-manager \
  python -m unittest discover -s benchmarks/clickbench -v
```

On a disposable Linux systemd host, also run the actual memory-kill and timeout
containment tests (no Redis database required):

```sh
CLICKBENCH_SYSTEMD_TEST=1 python -m unittest discover \
  -s benchmarks/clickbench -p test_isolation_linux.py -v
```

## Full-scale reference generation (#313)

Generate all 43 independent reference values from a typed DuckDB view over the
Parquet file. This streams the source and permits DuckDB disk spilling rather
than retaining a 100-million-row Arrow table. DATE integers represent epoch
days; TIMESTAMP integers represent epoch seconds. Reading the raw integers
with `read_parquet()` alone is not the same SQL schema.

```sh
uv run --no-project --with pyarrow --with redis --with adbc-driver-manager --with duckdb \
  python benchmarks/clickbench/references.py \
  --upstream /tmp/ClickBench --parquet /tmp/hits.parquet \
  --output full-reference --temp-directory /mnt/duckdb-tmp --memory-limit 16GB
```

The generator checks all 105 columns, source row count and required NULL
constraints, and records source SHA256, schema SHA256, upstream commit, query
number/SQL and DuckDB version. `manifest.json` becomes complete only when all
43 queries succeeded. Error records remain distinct from driver failures.
Validation rejects incomplete manifests, old untyped reference files, changed
sources, changed SQL or mismatched metadata. Regenerate old reference sets.
Use `validate.py --reference-dir full-reference` with the same source/upstream.
For different LIMIT selections, the validator independently checks membership in
the complete typed DuckDB result, including all projected aggregate values and
row multiplicity. Ordered results must fill exactly the required rank bands,
including partial ties at both OFFSET and LIMIT boundaries. Q25 retains its
unprojected EventTime sort key in the oracle. Different valid ties pass; missing
higher-ranked rows, invalid groups, incorrect aggregates and out-of-bound rows
fail. Oracle failures, unsupported ordering expressions, or more than one million
eligible tie rows remain `needs_tie_validation`; `compare.py` excludes them from
ratios. Exact ordered reference matches retain their order check.

Use `--tie-memory-limit 4GB --tie-temp-directory /mnt/duckdb-tmp` to bound oracle
memory and permit disk spilling. Correctness evidence records source/schema
provenance, the oracle version, raw trial hashes and the validation method.
The [October 14 validation evidence](tie-validation-2026-10-14/evidence.json)
resolves all 21 pending ties in the historical d0792283 run: 111 valid trials and
18 deadlines. These timings describe that original driver commit.

## Effective FinOps protection (#312)

Owner and skip-deletion tags did not protect the October 13 runner from the
external `finops_access_key` caller. Before another long AWS run, identify
**every** cleanup principal from CloudTrail and the account's cleanup inventory.
The external process source is still an operational investigation; modifying
one Lambda does not establish that another caller respects its exemptions.

Prepare a protected configuration file with these fields (use verified IDs):

```json
{
  "account_id": "123456789012",
  "region": "us-east-1",
  "run_tag": "unique-approved-benchmark-run",
  "instance_id": "i-0123456789abcdef0",
  "expires_at": "2026-10-14T08:11:41Z",
  "finops_principals": ["arn:aws:iam::123456789012:user/finops_access_key"],
  "cleanup_principal": "arn:aws:iam::123456789012:role/benchmark-budget-cleanup",
  "cleanup_rule_name": "benchmark-budget-cleanup",
  "cleanup_function_arn": "arn:aws:lambda:us-east-1:123456789012:function:benchmark-budget-cleanup",
  "redis_subscription_id": 123,
  "redis_database_id": 456
}
```

The deadline shown is illustrative and expires; replace it with the approved
spending deadline. Add the verified `instance_id` to the runner placement file.
After provisioning the independent scheduled cleanup and tagging the runner:

```sh
uv run --no-project --with boto3 python benchmarks/clickbench/finops.py \
  finops-config.json --install --output finops-verification.json
```

This installs only `ec2:TerminateInstances` explicit denies on the configured
FinOps principals, scoped to account, region, exact BenchmarkRun tag and expiry.
The cleanup role never receives this policy. Each installed policy is read
back, and actual principal policies are simulated for protected, unrelated,
untagged and expired cases; the budget cleanup role must remain allowed before
and after expiry. No live instance is deleted by the verifier. The preflight
checks the live runner tag/state and enabled EventBridge schedule, Lambda target
and execution role. It records exact Redis IDs; operators must separately verify
that the scheduled cleanup uses those owned IDs and valid Redis control-plane
credentials. IAM simulation does not attest external process source, unknown
principals, every organization control, or future policy changes. See the
[AWS simulator documentation](https://docs.aws.amazon.com/cli/latest/reference/iam/simulate-principal-policy.html).

Full AWS `run.py` executions require `--finops-config finops-config.json` and
`--with boto3`; the harness repeats the live preflight before connecting/loading
and saves the evidence and policy expiry in run metadata. Verification failures
stop the run. Deployment should verify exemptions before expensive Redis
provisioning too. Preserve this evidence with the deployment and ensure teardown
removes only the owned runner/database/subscription. After the deadline and
teardown, use `finops.py ... --remove-expired --output removal.json` to remove the
matching per-run policies; differing policy contents are never overwritten or
removed. This PR supplies the reusable guard; it does not modify live IAM or
claim the external FinOps process has been repaired.

## Interrupted-trial stage evidence

`trial.py` writes `qNN-T.progress.json` before connection/setup/execution/fetch/
output stages and refreshes it every five seconds. Failed isolated trials
retain it as `partial_diagnostics`, including the last active stage, completed
stage timings, elapsed wall time, CPU and process peak RSS. These partial
samples never replace null failure timings or establish completed query
latency. The sampler does not query the active ADBC connection and writes no
connection URI or result values. Read the [Q19 follow-up report](q19-followup/report.md)
for the timeout that motivated this and a reproducible exact-plan comparison.

## Dependency-ordered scheduled cleanup (#321)

Package `cleanup.py` in the independent cleanup Lambda and set its handler to
`cleanup.handler`, with `BUCKET` and `SECRET_ARN` environment variables. The
secret holds the Redis control-plane `key` and `secret`; never put credentials
in deployment state or logs. Lambda supplies `AWS_REGION`. Grant the role only
the required S3 state/status reads and state writes, Secrets Manager secret
read, EC2 describe/terminate/delete-security-group operations, and authorized
Redis resource deletion access. Preserve backup and result objects.

Persist these verified identities in `setup/deployment-state.json`:

```json
{
  "name": "unique-approved-benchmark-run",
  "region": "us-east-1",
  "instance_id": "i-0123456789abcdef0",
  "security_group_id": "sg-0123456789abcdef0",
  "security_group_name": "unique-approved-benchmark-run-runner",
  "vpc_id": "vpc-0123456789abcdef0",
  "subscription_id": 123,
  "database_id": 456,
  "database_name": "benchmark-database",
  "cleanup_deadline_utc": "2026-10-14T08:11:41Z"
}
```

The date is illustrative; use the approved deadline. Tag both the runner and
its dedicated security group with `BenchmarkRun` equal to `name`; the runner
also needs `Name` equal to `name` plus `-runner`. Untagged, shared, or mismatched
resources fail closed. Existing deployments must reconcile and verify these
identities and tags before adopting this module. Invoke with `verify_only=true`
for a read-only identity check; this does not attest IAM permissions or prove
that future deletion will succeed.

Each scheduled invocation advances one asynchronous step, beginning at the
approved deadline or when `results/status.json` has both `phase=complete` and
`artifacts_persisted=true`:

1. Terminate only the exact tagged runner; tolerate an already-absent instance.
2. Wait for its security group's network interfaces to disappear, delete only
   the exact tagged group, and verify absence on a later invocation.
3. Delete the exact database and verify its absence.
4. Require an empty matching subscription, delete it, and verify absence.
5. Persist `cleanup_verified=true` only after all targeted resources are gone.

The managed subscription can own the VPC: an extra benchmark security group
can prevent that VPC's teardown even when the database is gone. Never delete
the managed VPC directly, remove unrelated groups, or detach other workloads'
interfaces to force cleanup. Three security-group dependency violations produce
`action_required`; Redis `destroy_error`, failed tasks, or uncertain requests
also require intervention. Each pending phase escalates to `action_required`
after 30 persisted observations, including stuck ENIs and a completed task whose
resource remains present. Accepted EC2/group mutations are checkpointed before
submission and never repeated on stale readback. Task `processing-completed`
alone does not prove absence. Submitted tasks are checkpointed and never blindly repeated. Monitor
`cleanup_phase`, `pending`, and `action_required`; notify on actionable failure
and disable the schedule only after verified cleanup. Deployment adapters and
schedule provisioning remain operator-managed; this PR does not alter a live
Lambda, infrastructure, or budget.

Run the cleanup regressions without cloud access:

```sh
python3 -m unittest discover -s benchmarks/clickbench -p test_cleanup.py -v
```

## Local partition API comparison

`partition_profile.py` is a reduced-data comparison of ExecuteQuery and
ExecutePartitions/ReadPartition, separate from the Cloud leaderboard runner.
Use a disposable local Search server and the official source Parquet prefix.
It records all 43 official queries' partition support, then times supported
queries and clearly labeled supplemental row-local workloads. Full results are
checked against DuckDB/source values and ordinary Arrow schemas.

```sh
REDIS_URI=redis://127.0.0.1:16390/0 uv run --no-project \
  --with pyarrow==26.0.0 --with adbc-driver-manager==1.12.0 \
  --with duckdb --with redis --with psutil \
  python benchmarks/clickbench/partition_profile.py \
  --driver go/build/libadbc_driver_redis.dylib --upstream /tmp/ClickBench \
  --parquet /tmp/hits-prefix.parquet --rows 100000 --repeats 3 \
  --partitions 1 4 8 --workers 1 4 --output /tmp/partition-profile.json
```

Choose a fresh output path. `--layout sparse` retains every tenth allocated ID;
`--layout skewed` retains the final tenth. Each run drops only its generated
table/schema. Use the host's `.so` or `.dylib` binary as appropriate. This pilot
has no Linux cgroup memory enforcement, no cold-cache claim and no Cloud/full
scale ranking. Timing includes fresh worker startup/connections, full fetch and
IPC output for both methods. RSS/server-memory sampling and network deltas have
the measurement limits recorded in every report.

The comparison waits for `FT.INFO` document count and `indexing=0` after loading
and layout changes, before timing reads. SQL counts alone are insufficient.
Failures retain JSON diagnostics and complete actual/expected Arrow outputs.
Use `--workloads filter_adv` for a focused reproduction and
`--keep-failure-table` only on the dedicated server when retaining the generated
failed table is useful. That option prints the retained table name; the owner
must clean it up. See the [dated evidence](../partitions/evidence-2026-10-21/README.md).

Add `--strategies rowid balanced` to compare the default split with bounded
live-row balancing using identical worker startup and result/schema checks.
The strategy applies only to partition trials. `--workloads scan_narrow scan_wide`
can focus the comparison; `--layout skewed` retains the final tenth of allocated
IDs. Count-probe planning is included in end-to-end timing.
