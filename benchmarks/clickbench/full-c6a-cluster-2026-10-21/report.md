# Full-scale ClickBench: in progress

User-authorized full 99,997,497-row run, started 2026-10-21.

Latest checkpoint: 2026-10-21T20:36:31.548515+00:00 (UTC).

- Phase: **running**.
- Rows loaded: **99,997,497 / 99,997,497 (100.0%)**.
- Completed query trials: **33 / 129**.
- Redis used memory: **756.9 GiB** total.
- Busiest primary: **15.98 / 24 GiB**.
- Unreachable nodes: 0.

48 Redis primary instances and one dedicated ADBC client, all c6a.4xlarge.
Total: 784 vCPUs and 1,568 GiB RAM. Redis maxmemory: 1,152 GiB; no replicas.
This is a larger aggregate allocation than a single-instance published baseline.

All nodes, volumes and the security group have the required lowercase owner,
RedisLabsIdentifier, BenchmarkRun, skip_deletion and expiry tags. EC2 API
termination protection is enabled. Independent cleanup is scheduled for
2026-10-22 07:16 UTC (October 22, 12:16 a.m. Pacific). API protection is not an
IAM deny; authorized attribute changes can remove it. A live termination attempt
was rejected before the load.

[Placement](placement.json), [initial live preflight](finops-verification.json),
[termination test](termination-protection-probe.json), [checkpoint](status.json).
The independent DuckDB references are complete and match the downloaded full
source SHA256 a390f6cb782f6aaef278c72fc1dd86c4f30bc843ebab3c159e9bd4d45ddb079f.

AWS checkpoints: `adbc-clickbench-adbc-main-20261014-1936-735486936198/full-c6a-20261021/`.
Full-scale timings and correctness results are pending; no ranking is claimed.
