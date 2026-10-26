# Specification

Preserve the Cloud runner's default endpoint restriction and existing full-AWS
FinOps checks. Add explicit self-hosted mode using REDIS_URI with a loopback
seed and a finite budget deadline. Auto-detect OSS Cluster and check FT.INFO on
all primaries; every coordinator must report the expected global count and
finished indexing before timing. Do not sum coordinated global counts.
Cluster resume is unsupported and must fail explicitly.

Use official PostgreSQL ClickBench SQL and typed source rows. Keep failures
visible and independently validate successful query values against DuckDB.
A reduced-data pilot cannot be ranked against published full-data results.
Infrastructure: six c6a.4xlarge nodes (96 vCPUs, 192 GiB total), one AZ, no
replicas, private cluster ports, operator-only SSH, ephemeral disks and keys,
and a three-hour instance shutdown/termination deadline.
See [intent](intent.md) and [plan](plan.md).
