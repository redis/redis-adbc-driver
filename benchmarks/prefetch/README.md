# Bounded prefetch screen

This runner isolates the `scanprefetch` experimental build from the same source
with the tag disabled. It does not load ClickBench data. Restore an existing
verified compatible full-scale backup on Redis Software and verify active 4x QPF.
Keep deployment resources and credentials in private operational records.

Build `main.go` from each checkout's `go/` directory with the same Go version
and linker flags. For example, substitute an absolute harness path:

```sh
go build -ldflags '-s -w' -o /tmp/baseline-screen /absolute/path/main.go
go build -tags scanprefetch -ldflags '-s -w' -o /tmp/candidate-screen /absolute/path/main.go
```

The executable uses the **public Go ADBC APIs and Go Arrow allocator**, not the
C shared-library boundary. Both methods preopen connections, then measure public
query execution or partition planning, complete fetch, IPC output and stream/
statement cleanup. Database/connection construction and independent validation
are outside the reported inner interval. The outer process interval includes
initialization and teardown. Partitions always request four descriptors; one or
four concurrent readers consume them. Four ordinary readers use disjoint exact
internal-ID ranges. Both return the same 100K typed rows.

Prepare `source.arrow` as an Arrow **file** from the independent official
Parquet, using the pinned PostgreSQL DDL and existing
`benchmarks/clickbench/run.py` `arrow_schema`/`convert_batch` helpers. Retain the
Parquet SHA-256, DDL revision and actual conversion code as provenance; do not
derive the reference from either driver output. Normalize names to lowercase.
For a restore loaded with parallel writers, internal-ID order is not necessarily
Parquet order. Read membership independently from the authoritative HASHes,
validate exact internal IDs and unique WatchIDs, and select the corresponding
typed official-source rows. Require exactly 100K source matches; obtain **all
105 expected values from the official source**. Preserve the membership-selection
code and its provenance. A serial prefix is valid only with verified source-order
loading. Never reinterpret an output mismatch as expected parallel-load behavior.
`reference.py` implements this read-only selection and typed-source join, verifies
the complete Parquet SHA-256, and rejects missing/duplicate membership. It needs
DuckDB as well as PyArrow/redis-py; point `--helpers` at the tracked ClickBench
helpers and `--ddl` at the pinned official PostgreSQL DDL.

A private JSON configuration supplies `uri`, `ca`, `namespace`, `source`,
`source_sha256`, `source_provenance`, `batch_rows` (65536),
`binaries` (`baseline`/`candidate` absolute executable paths),
`verified_software_qpf_4x: true` and `qpf_probe` (argv of a private read-only
management API verifier). The verifier runs before every trial and must return
JSON containing `verified: true`, `factor: 4`, `search_workers: 6`,
`sched_policy: "mnp"`, `conns: 32`; it should also identify distribution/version
and active shard count. Do not pass credentials on the command line. The config
is sent to the Go executable over stdin and is not copied into the report.

```sh
python screen.py --config /private/path/config.json --output /private/path/direct
python screen.py --config /private/path/config.json --output /private/path/wire \
  --wire-diagnostic --smoke --repeats 1
python -m unittest discover -s . -p 'test_*.py' -v
```

Python requires PyArrow and redis-py. Direct runs have one warm-up and five
measured alternating-order rounds for wide/narrow, ordinary/public-partition
APIs and one/four readers: 96 executions. Every execution writes its own raw IPC
files and independently validates all values, Arrow types and duplicate
multiplicity outside timing. Errors and their raw outputs remain in that run;
an output directory cannot be reused. `--smoke` results are ineligible for a
promotion decision.

Wire diagnostics use a separate TLS-transparent loopback relay, with the same
full-value validation. Trust and hostname verification stay enabled (the server
certificate must cover 127.0.0.1). Request/reply counters include TLS setup and
framing and exclude TCP/IP headers and the separate INFO control connection.
Proxy timings are not combined with direct timings. Server INFO counter deltas
are retained with their endpoint scope, not misrepresented as a per-shard CPU
decomposition. Client CPU covers the whole Go process during the inner interval.
`TotalAlloc` and allocation-count deltas include Arrow and IPC encoding; sampled
Go heap (20 ms) is not a strict peak bound or RSS. Per-process lifetime peak RSS
includes initialization. These allocator/consumer details limit comparisons with
Python/C Arrow results and must accompany any reported speedup.
