# Exact aggregate comparison (#406)

`compare.py` runs ordinary SQL with internal aggregate workers (not public ADBC
partitions), fully consumes Arrow output and checks exact values against an
independent DuckDB oracle over the preserved Parquet file. Integer sums/counts
use HUGEINT and Fraction; AVG is rounded once. Q9/Q10 are checked against every
reference group, including the full rank sequence and admissible tied cutoffs.

Supply two existing native libraries, the immutable source file, a new work
directory, the loaded schema and a JSON provenance file. The runner never loads
or changes Redis data. `REDIS_URI` is read by the supervisor; trial credentials
cross stdin and are not written to jobs/results. Diagnostic files include SQL
and are not general-purpose public telemetry.

```sh
python compare.py --baseline /absolute/baseline.so --candidate /absolute/candidate.so \
  --parquet /absolute/hits.parquet --work /absolute/new-results \
  --schema fixture --provenance /absolute/build-provenance.json --repeats 3
```

Default scope is the official 100,000-row/105-column prefix. Full
99,997,497-row runs additionally require the reserved recovery/backup/restore
gate, exact source checksum, 48-primary topology, Linux systemd/cgroup-v2 memory
isolation and the fixed experiment cutoff. The runner journals trial unit names
before launch. The operator must also journal its supervisor unit, mirror the
remote ownership journal to the coordinating chat's local gate, preserve the S3
report, verify no active jobs and release the window before cleanup. A deadline
or missing gate never grants authorization to start or extend the window.

Each build/query runs in a fresh process; pair order alternates by round.
Connection/setup is outside query elapsed time; elapsed includes execute and
complete Arrow fetch. CPU covers the worker process including connection/setup.
Server INFO deltas include monitoring/control traffic. Memory is process peak RSS
and sampled maximum primary RSS/used memory, not a hard Redis memory bound.
Full trials additionally report actual cgroup memory peaks/failures.

[Evidence and exploratory reports](2026-10-21/README.md).
