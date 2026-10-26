# Arrow IPC and finite streaming

The driver serializes Arrow IPC **inside the native driver**, through standard
ADBC statement options. No client-side serializer, new C entry point, Redis wire
format, or custom Arrow C Stream layout is required. This implements the IPC
portion of [#227](https://github.com/alberttwong/redis-adbc-driver/issues/227).
Continuous subscriptions and CDC are tracked separately in
[#457](https://github.com/alberttwong/redis-adbc-driver/issues/457).

## Capability matrix

| Capability | Support | Boundary and limits |
|---|---|---|
| Arrow C Stream query results | Yes | Standard ADBC `ArrowArrayStream`; an in-process interface, not an IPC byte format |
| Arrow C Stream ingestion | Yes | Standard ADBC `BindStream` with bulk-ingest options |
| Arrow IPC streaming format | Yes | Native local-file export/import through statement options; schema followed by sequential batches |
| Arrow IPC file format | Yes | Native local-file export/import; import requires a seekable regular file; footer metadata grows with batch count |
| Finite incremental query output | Eligible indexed single-table SELECTs and UNION ALL trees | Cursor pages fetched as batches are consumed; [eligibility and cleanup](driver-reference.md#streamed-results) apply |
| Incremental aggregate input | Yes for eligible table scans | Page-wise reduction from #212; output still waits for reduction and state grows with groups/distinct/ordered-set inputs |
| Continuous subscriptions / CDC | Unsupported | Separate scope in #457; no checkpoint, replay, or snapshot-to-live contract |

## Statement contract

| String option | Default | Meaning |
|---|---|---|
| `adbc.redis.ipc.format` | `stream` | Exactly `stream` or `file`; selects encoding for input or output |
| `adbc.redis.ipc.output` | Empty (disabled) | Absolute local destination path that does not already exist |
| `adbc.redis.ipc.input` | Empty (disabled) | Absolute local regular-file source path for bulk ingestion |

Set options using `AdbcStatementSetOption`. Execute using
`AdbcStatementExecuteQuery(statement, NULL, &rows, &error)` in C, or
`execute_update()` on a Python low-level ADBC statement. A non-null output stream
or partition execution with IPC paths configured is rejected: use a separate
statement for ordinary Arrow results. The paths and format persist until changed;
set a path to the empty string to disable it.

Export requires exactly one SELECT, including SELECTs with blocking plans, and
returns the fully written row count. Scripts, DML and ingest-target export are
rejected before execution. Bound SELECT parameters work through the normal ADBC
binding interface. Existing destinations, including symlinks, are never replaced.
Output is created with owner-only permissions (subject to the process umask).
A failed export removes its partial file. Consumers must wait for success before
opening/publishing it: there is no atomic publication, fsync, or crash cleanup
guarantee. Disk errors and late query errors fail the operation.

Import requires `adbc.ingest.target_table` and the normal ingest mode/schema
options, with no independently bound data. Input/output cannot both be enabled.
The native IPC reader feeds the existing ingestion path one batch at a time.
An empty input preserves its schema and can create an empty table. SQL type
support and schema validation remain those of ordinary Arrow ingestion. Invalid
encoding/schema/batches fail normally. Ingestion autocommits: a late decode,
Redis, or cancellation error can leave the table and earlier rows written.

Paths refer to the filesystem of the process loading the driver, with that
process's permissions. Network URLs, pipe transport and automatic format
detection are not provided. Treat IPC sources as trusted local files; there is
no new decompression or allocation quota for imported Arrow data.

## Executable example

Install the Python package (the driver performs serialization; this example does
not require PyArrow). `--output` must name a new file. Optional `--ingest-table`
creates a new table from that output through native ingestion:

```sh
python python/examples/arrow_ipc.py \
  --uri 'redis://localhost:6379/0' \
  --query 'SELECT id, name FROM example' \
  --output /absolute/new-result.arrow --format stream
```

The core calls are also usable from any ADBC C binding:

```python
import adbc_driver_manager as adbc
from adbc_driver_redis import connect

with connect("redis://localhost:6379/0") as db, adbc.AdbcConnection(db) as conn:
    with adbc.AdbcStatement(conn) as stmt:
        stmt.set_sql_query("SELECT id, name FROM example")
        stmt.set_options(**{
            "adbc.redis.ipc.output": "/absolute/new-result.arrow",
            "adbc.redis.ipc.format": "stream",
            "adbc.redis.stream_batch_rows": "4096",
        })
        rows = stmt.execute_update()
    with adbc.AdbcStatement(conn) as stmt:
        stmt.set_options(**{
            "adbc.ingest.target_table": "example_copy",
            "adbc.ingest.mode": "adbc.ingest.mode.create",
            "adbc.redis.ipc.input": "/absolute/new-result.arrow",
            "adbc.redis.ipc.format": "stream",
        })
        ingested = stmt.execute_update()
        assert ingested == rows
```

## Native IPC performance

The [matched native-versus-PyArrow benchmark](../benchmarks/native-ipc/2026-10-22/README.md)
covers both IPC formats, export/import, narrow/wide scans, UNION ALL and blocking
aggregation. At the tested local scale, native elapsed medians differed by
-3.3% to +3.8%, with all trial ranges overlapping; no speed advantage was
established. See the report for CPU, RSS, encoded size and scope limits.

## Incremental execution and memory

IPC export consumes each Arrow batch before requesting the next. The serializer
adds batch/dictionary buffers; it does not make a blocking SQL plan incremental.
IPC file writers also keep footer metadata proportional to batch count. Import
similarly holds the current decoded batch rather than collecting a complete
Arrow table; one unusually large batch can still require substantial memory.
Cancellation is checked between batches, and the underlying query/ingest checks
continue to apply. A local read/write already in progress must return before it
can observe cancellation. Readers, writers and files are released on success
and error, including early query failure and late batch failure.

| Query family | Existing input/output behavior | Further incremental output assessment |
|---|---|---|
| Indexed row-local filter/projection, eligible LIMIT/OFFSET and exact index order | Incremental input and output | Already streams; direct Arrow projections and byte budgets remain applicable |
| Aggregates/GROUP BY | Eligible scans reduce input page by page; output after reduction | General groups cannot be finalized before complete input; group/distinct state remains cardinality-dependent |
| Driver ORDER BY | May require complete input; eligible top-K uses a bounded heap | Top-K still needs all candidate input before output; bounds projected winners, not every upstream operator |
| DISTINCT and distinct set operations | Retain deduplication/comparison state | Emission-order and SQL duplicate/type rules need a separate executor design; no new path enabled here |
| Eligible UNION ALL trees without ORDER BY | Incremental output over sequential managed indexed-table child scans | Pairwise coercion and per-node LIMIT/OFFSET; only one active child; unsupported leaves/orderings retain materialization |
| Joins, windows, CTEs/derived tables | Existing materialization paths | Streaming requires operator-specific state and dependency analysis; no blanket eligibility expansion |

Native IPC provides an incremental sink/source. [#459](https://github.com/alberttwong/redis-adbc-driver/issues/459)
adds the eligible UNION ALL path described in [streaming documentation](driver-reference.md#streamed-results),
with [fresh measurements](../benchmarks/union-stream/2026-10-22/README.md).
Neither feature adds spill or a query-wide memory cap. Existing output byte
budgets apply only to eligible streamed batches. They do not bound process RSS,
consumer-held buffers, server sort/group state, or all blocking operators.

Historical evidence, not measurements of this change:

- [Aggregate pages](../benchmarks/aggregate-pages/report.md): at 100K rows,
  page-wise Q9 reduction measured 141.1 MiB peak RSS versus 348.2 MiB before;
  high-cardinality Q33 still used 622.7 MiB. This demonstrates why incremental
  input does not imply constant-memory aggregation.
- [Stream byte budgets](../benchmarks/stream-byte-budget/2026-10-21/README.md)
  records batch accounting, cursor-page overshoot, peak memory, cancellation and
  release coverage. These budgets do not cap blocking operators.
- [Incremental read comparison](../benchmarks/incremental-read/2026-10-22/README.md)
  records independently validated complete IPC transfers with different row
  ceilings and explicit SQL ranges. It predates these native IPC options.

Other candidates are explicitly deferred in #459: general aggregates require
complete input before finalization; DISTINCT set operations require cardinality-dependent
deduplication state; joins/windows and CTE/derived-table dependency graphs need
separate operator designs. Top-K can bound retained winners while still consuming
all candidates. No new spill engine or limit for those operators is added.
Further eligibility expansion requires its own correctness, lifecycle and memory evidence.
CDC remains exclusively in #457.
