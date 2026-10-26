# Specification: matched IPC comparison

Date: 2026-10-22. Author: Codex. Base: `e2e7725`. Status: measured; review pending.
See [intent](intent.md) and [plan](plan.md).

Four export workloads and two import workloads, each with stream/file format,
native/PyArrow method, one warmup and five measured fresh-process trials: 144
validated operations, of which 120 enter reported medians. Alternate method
order within pairs. Default fixture size: 50,000 rows, 3-column narrow and
32-column wide. Mixed scalar types include exact integers/decimals, UTC
timestamps, binary, unicode/NUL strings and nulls. Source and imported tables
use the same default index policy. Client query workers=1, output/input batches
4,096 rows and byte budget disabled. Aggregate pushdown=none for both methods.

Export compares native `adbc.redis.ipc.output` + execute_update against
execute_query -> Arrow C Stream -> PyArrow IPC writer. Native export requires
a fresh path; every client output is also a fresh file on the same filesystem.
Import compares native `adbc.redis.ipc.input` against PyArrow OSFile reader ->
BindStream. Both consume the exact same source file and create a new target
table, including index creation. File-reader adapters yield one batch at a time.

Connection, statement/options and fixture setup precede timing. Timed export
ends after complete output file close; timed import ends after all batches are
ingested and its input reader/file is closed. Process user/system CPU deltas
and peak RSS are captured immediately then, before validation or statement
teardown. Record pre-operation RSS high-water baseline without subtraction.
Metrics exclude source creation, connection setup, validation, teardown, server
CPU/RSS and fsync. Local filesystem page cache remains enabled. There is no
cold-cache, crash-durability or time-to-first-byte guarantee.

Each export is read and compared column-for-column and row-for-row against an
independently generated source Arrow table; UNION ALL expects exactly two
copies, aggregates use arithmetic counts/sums by bucket. Each import is fully
queried back and checked against its source. Exact numeric and schema checks
are required; hashes alone do not establish correctness. Save file sizes,
schema hashes, raw trials, source/binary/input checksums and environment.
