# Redis Terra driver benchmark attempt — 2026-11-10

**Outcome: blocked by missing Redis Search.** The supplied Terra instance was
reachable through the approved VPC peering and negotiated RESP3. The current
driver failed during connection initialization, before creating benchmark data:

```text
Not Implemented: [redis] the Redis server does not provide the search (RediSearch) module: ERR unknown command 'FT._LIST'
```

The native command probe independently confirmed that `FT._LIST`, `FT.CONFIG`,
and the Search commands inspected through `COMMAND INFO` were unavailable.
`PING` returned `PONG`; `CONFIG GET maxmemory-policy` returned `noeviction`.
The server reported version `99.99.99` and cluster mode. That reported version
does not identify a public Redis release.

## Reproduction and scope

- Driver commit: `73eb96420a20a4afc6b33a38764943be3b2ebf8c`.
- Client: one temporary AWS `c7i.xlarge` in us-east-1, 4 vCPUs and 8 GiB RAM.
- Interface: the driver's native Go ADBC API, compiled for Linux amd64 with
  the module's Go 1.27.3 toolchain and its normal version linker flag.
- Telemetry disabled; four query workers configured.
- Connection used the supplied endpoint through private VPC peering. No inbound
  runner ports were opened. Temporary remote execution used an approved role
  containing only `AmazonSSMManagedInstanceCore`.
- The compiled [harness source](harness.go), executable SHA-256, and sanitized checks
  are recorded alongside [results.json](results.json).
  Operational identifiers, endpoint details, control-plane records, and raw
  console output remain outside the repository.

The harness called `NewDatabaseWithContext` followed by `Open`. The driver
requires a successful `FT._LIST` before opening its snapshot catalog; see
[database initialization](../../../go/database.go). It returned the error above.
The single failed connection attempt took 43 ms; this is diagnostic evidence,
**not a successful-query latency measurement**.

The harness is archived source from this attempt. Its workload paths were not
reached on Terra. A failed connection is reported as `driver_open.ok=false` in
JSON even when the process exits zero; process success does not establish
compatibility. Reruns require a dedicated non-production database because a
successful connection proceeds to create and later drop its synthetic schema.

## Planned workload and unmeasured results

The compiled harness was prepared to bulk-ingest deterministic four-column
Arrow fixtures at 10,000 and 100,000 rows, then run a warm-up and five trials of
each query. Planned queries covered point lookup, indexed filtering with top 10,
range count, grouped count/sum, exact int64 sum, fetching 10% of rows, full Arrow
scan, and ordered top 10. Timers covered execution and complete Arrow fetch;
exact result-value validation was planned outside the timer.

No ingestion or query trials ran, and no benchmark schema or table was created.
There are no throughput, query latency, or speedup results. ClickBench was not
run. This is Terra compatibility evidence, not Redis Software performance
evidence; active 4x QPF was not established on Terra. The current driver remains
autocommit-only, so Terra transaction semantics were not exercised.

## Deployment and cost

The supplied, existing Terra database's console showed Standard tier, 1.5 TiB
dataset capacity, 153.6 GiB RAM, 19.2 vCPUs, 16 shards, and two replicas in a
single zone. Its displayed estimated price was $22.79/hour; this work did not
create or resize that database.

AWS's Price List API quoted $0.1785/hour for the temporary Linux runner.
Temporary resource ownership, cleanup verification, and estimated compute cost
are recorded in `results.json`. That compute estimate excludes EBS, public
IPv4, S3 requests/storage, and network fees. The existing VPC and Terra peering
are retained.

## What enables a driver benchmark

The target must expose the Redis Search capabilities required by the current
driver. If Terra does not offer them, running this driver's SQL workload on
Terra requires a separate backend implementation. Native Redis key/value or
transaction measurements would be a different benchmark and would not establish
this driver's performance.
