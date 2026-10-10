# ClickBench on Redis Cloud

Run the official ClickBench PostgreSQL query variant through Redis ADBC on a
colocated Redis Cloud deployment. The harness loads all 105 columns, isolates
queries in separate processes, records failures, and validates outputs against
DuckDB references before reporting latency ratios.

See [loading-improvements.md](loading-improvements.md) for the loading changes,
local pilot measurements and validation. Operational deployment records and
live benchmark artifacts are kept outside the reusable harness.

## Deployment and placement

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
limit is independent and remains visible as a query error. The driver outside
this benchmark harness does not acquire a hard per-query memory limit.

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
