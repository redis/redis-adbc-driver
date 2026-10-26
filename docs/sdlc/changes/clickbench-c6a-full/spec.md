# Specification

Use 48 c6a.4xlarge Redis primaries, no replicas, plus one dedicated c6a.4xlarge
client initially. Expand further only if measured memory requires it. Initial
allocation: 784 vCPUs / 1,568 GiB host RAM, with 1,152 GiB Redis maxmemory.
All nodes share use1-az6. Use the pinned Redis 8.6.2 image and unchanged driver
source, official PostgreSQL SQL and the full hits_compatible Parquet.

Use the complete typed DuckDB references at upstream e90045879276f75cc398f1ab8a73541a7716058d.
Verify the actual complete source SHA256, DDL SHA256, source row count and every
query identity against that manifest before loading. Keep all errors and null
failure timings. Independently verify LIMIT ties before including ratios.

Verify lowercase owner, RedisLabsIdentifier, BenchmarkRun, skip_deletion and
ExpiresAt tags on every node, root volume and owned security group. Read the
account cleanup source; do not assume owner tags protect against all callers.
Keep API termination protection enabled on every node and an independently
scheduled cleanup capable of removing that flag at the twelve-hour deadline.
This is an explicit API-attribute alternative; it does not claim an IAM deny.
Retain the existing Cloud IAM preflight behavior. Verify every cleanup target,
code hash, role, exact IDs and expiry before loading. Short-lived operator SDK
credentials for the preflight cross encrypted SSH stdin and a root-only tmpfs
FIFO, never arguments, environment, disk files or logs. Runtime S3 checkpoints
use the existing restricted benchmark instance profile.

After timed queries, snapshot all 48 primaries while the dataset is quiescent.
Check each RDB structurally and with SHA256, verify uploaded sizes/checksum
metadata, and save original Redis node IDs, slot maps, per-node key counts,
catalog namespace, pinned image, driver artifacts, full Parquet and typed
references in a separate retained S3 backup prefix. Publish a complete manifest
only after every required object is uploaded. Keep restore-test status explicit;
export/checksum success alone is not restoration success. Test restoration
before teardown when the remaining cleanup budget allows it. The destination
bucket is versioned, has no automatic expiration rule, and retains required
owner/reaper tags. Retention is user-managed; teardown must not delete backups.

Keep incremental results and progress in the dedicated full-c6a-20261021 prefix
of the existing benchmark bucket. Stop and clean up owned infrastructure after
artifacts are preserved, or at the fixed deadline. See [intent](intent.md) and
[plan](plan.md).
