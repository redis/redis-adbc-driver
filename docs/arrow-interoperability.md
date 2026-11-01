# Arrow normalization, layouts and ownership

This is the source contract for [issues 198–209](https://github.com/redis/redis-adbc-driver/issues/198).
Redis HASHes store SQL values; an input Arrow encoding is not a persistent
storage layout. Rebuild the native library to use these changes.

## Input and stored schemas

| Input family | Stored SQL value and default output | Physical output selection |
|---|---|---|
| uint8, uint16, uint32 | SMALLINT, INTEGER, BIGINT respectively | Signed output; no unsigned type identity |
| uint64, including `UINT64_MAX` | Exact NUMERIC(20,0), Decimal128(20,0) | Decimal128 or Decimal256; never float during ingestion/storage |
| Decimal32/64/128/256 | NUMERIC with input precision and scale; Decimal128 up to precision 38, Decimal256 above | Decimal width can be selected if declared precision fits |
| Date64 | DATE; floor milliseconds to epoch days, including negative/non-midnight values; Date32 output | Date64 at midnight; discarded time of day cannot be recovered |
| Time32/64 | TIME with supported unit | Canonical Time32/64 output |
| Timestamp without zone | Naive SQL timestamp, same unit | Canonical naive timestamp |
| Timestamp with zone | SQL instant with same unit and UTC output | Explicit output zone changes metadata, preserving epoch ticks |
| MonthInterval, DayTimeInterval, MonthDayNanoInterval | Calendar INTERVAL with exact months/days/nanoseconds | MonthInterval or DayTimeInterval only when representable |
| Duration | INTERVAL with nanoseconds and no calendar components; unit conversion checks int64 overflow | Duration in an explicit unit, only with zero months/days and exact divisibility |
| String/LargeString/StringView | Owned UTF-8 SQL string; String output | String, LargeString, StringView |
| Binary/LargeBinary/BinaryView/FixedSizeBinary | Owned arbitrary bytes; Binary output | Binary, LargeBinary, BinaryView, or fixed byte width |
| Dictionary | Decode each index using that batch's dictionary, including NULL values/indices | Opt-in string/binary dictionaries described below |
| List/LargeList/FixedSizeList/ListView/LargeListView | Recursive, potentially ragged arrays; canonical List output | Input offset width, child field metadata and fixed width are normalized |
| Named Struct | Named stored records, recursively normalized child types | Struct output with stored child names and nullability |
| Map, dense/sparse Union | Rejected with unsupported-type error | No implicit JSON/text conversion |
| Extension types or `ARROW:extension:*` field metadata | Rejected by default; explicit allowlisted storage normalization below | No implicit fallback or persisted extension identity |

Decimals validate physical precision (9/18/38/76 digits); scale is retained,
including negative scales. Arrow scales must fit the existing SQL numeric
domain: at most 16,383 fractional digits and at most 131,072 declared integer
digits (`precision - scale`); excessive metadata rejects before value work.
SQL casts retain existing NUMERIC rounding rules. Arithmetic preserves the
legacy minimum precision 38 and promotes wider declared operands/results to
Decimal256, up to 76 digits; addition/subtraction reserve a carry digit and
multiplication reserves the combined operand precision. SUM reserves up to 19
additional coefficient digits for an int64 row count, capped at 76. Values
exceeding that capacity reject rather than silently round or use float.
Mixed uint64/signed arithmetic uses those same exact decimal rules; explicit
float casts, mixed float expressions and SQL decimal division can use floating
point, as does ordinary numeric AVG. SUM/MIN/MAX retain exact numeric values.
Appending uint64 to an existing signed integer column checks that
column's range and reports SQLSTATE 22003 on overflow. Creating from uint64
always chooses NUMERIC, even when the first batch contains only small values.
Existing table metadata is unchanged. Authoritative HASH decimal strings retain
all digits; Search numeric values never become the authoritative stored value.

Timestamp zones must be valid Arrow/IANA names or supported numeric offsets;
`Local` is rejected because its meaning depends on the reader's machine. Input
zone names normalize to UTC. Epoch ticks identify the instant at a DST overlap;
output zone selection does not reinterpret local wall time. TIME WITH TIME ZONE
remains a separate string plus driver SQL-type metadata convention.
Arrow time-of-day input must fit SQL TIME's 00:00:00 through 24:00:00 range;
negative/over-day ticks reject. Date64 inputs outside DATE's signed int32-day
domain reject instead of overflowing.

## Explicit result layouts

Set the statement string option `adbc.redis.result_layouts` to a JSON object
mapping **exact result column names** to layouts. The default is `{}`. Example:

```python
cur.adbc_statement.set_options(**{
    "adbc.redis.result_layouts": '{"amount":"decimal32","label":"large_string"}'
})
cur.execute("SELECT CAST(amount AS NUMERIC(9,2)) AS amount, label FROM orders")
result = cur.fetch_arrow_table()
```

Supported strings are `string`, `large_string`, `string_view`, `binary`,
`large_binary`, `binary_view`, `fixed_size_binary:N`, `dictionary`, `decimal32`,
`decimal64`, `decimal128`, `decimal256`, `date64`, `timestamp:ZONE`,
`month_interval`, `day_time_interval`, and `duration:s|ms|us|ns`.

The option is at most 64 KiB. Unknown/ambiguous result names and incompatible
source types fail. Decimal widths must fit the **declared precision**, not just
the values observed so far. Timestamp zone selection requires an already zoned
source. Fixed binary requires width 1–16,777,216 bytes and exact byte length for every
non-NULL value. DayTimeInterval requires zero months and exact, int32-range
milliseconds; MonthInterval requires zero days/nanoseconds. Duration requires
zero calendar months/days, and no truncation to the requested unit. Such value
errors can surface during reader consumption; always finish fetching and check
reader errors.

`ExecuteSchema` and returned batches use the same selected schema. Table schema
discovery continues to report the stored canonical schema. SQL expressions,
casts, comparisons, aggregates and append compatibility use canonical SQL types;
the result option is applied afterward. It does not change table metadata.
Each transformed batch allocates output buffers; this option is not a zero-copy
or throughput improvement. Consumer-retained batches survive reader advance and
release. The source batch and transformed batch may coexist in memory.

Dictionaries support String/Binary values with int32 indices, independently
rebuilt for each batch. NULL indices represent SQL NULL; unused entries and
original input dictionaries are not persisted. At most 65,536 distinct entries
are allowed per batch; exceeding this ends the reader with an error. Reduce
`stream_batch_rows` for high cardinality. Schema cannot switch to plain arrays
halfway through a stream. Dictionary payload bytes scale with the batch's
values, with at most **16 MiB of distinct dictionary string/binary payload per
batch**. Repeated values count once toward that payload limit. Exceeding either
limit ends the reader with InvalidArgument; reduce `stream_batch_rows`.
These limits exclude index buffers, bookkeeping, source batches and
consumer-retained batches and are not RSS caps. IPC stream output supports
replacement dictionaries across batches; dictionary IPC **file** output is
explicitly rejected because this policy does not provide a file-wide dictionary.

Other selected layouts support native IPC stream and file output. Live and
retained `ExecutePartitions` reject any nonempty result-layout option before
publishing work. Independent replay keeps its established schema/IPC validation
contract. Default canonical output remains available to both partition modes,
subject to their existing expression/type/descriptor limits.

## Recursive arrays and stored records

Arrow arrays preserve ragged shape, NULL containers, empty containers and NULL
elements. Indexing/slicing operates on the current outer list, with existing SQL
one-based bounds; nested lists can be read by repeated subscripting. This does
not add PostgreSQL rectangular multidimensional-array declarations, dimension
bounds, or multidimensional SQL array constructors. Existing array functions
operate on the outer container. Offset sharing and nonzero slices are decoded
by logical offsets. LargeList normalizes to List; output batches must still fit
ordinary List offsets. No arrays or records receive Search indexes or nested
field pushdown.

Struct field names must be unique, nonempty, at most 1,024 bytes, and contain no
NUL. Structs have 1–1,024 children, a serialized type of at most 1 MiB, and Arrow
type nesting is limited to 16 levels. Names use normal SQL identifier resolution:
quote case-sensitive or special names in `(record_column)."Field Name"`.
Child nullability is retained and enforced for present parents. A NULL parent
ignores child payloads; an all-NULL present record is a non-NULL Arrow Struct.
The existing SQL record `IS NULL` behavior remains field-wise.

Named record append requires matching field names, order, normalized types and
nullability. There is no implicit field reordering or schema evolution. Anonymous
SQL `ROW` expressions keep their existing `f1`, `f2`, … output and restrictions;
they do not become stored composite types. Record comparisons remain positional
and lexicographic, using existing SQL record NULL rules. Nested arrays/records
use versioned binary-safe JSON cells in HASH values; scalar arrays retain their
previous text representation. Old readers do not understand new nested/record
metadata. Dropping/renaming a top-level column continues to operate on its one
HASH field, not individual struct children.

## Streaming inventory

| Operator/source | Incremental input | Incremental output / retained state |
|---|---|---|
| Eligible indexed single-table scan | Cursor or bounded row-ID pages | Batches; one page's decode/Arrow overshoot can coexist |
| Unordered prepared key lookup | Pipelined HASH chunks | Batches; prepared key list remains O(number of keys) |
| Eligible UNION ALL | One current leaf | Pairwise coercions and per-node LIMIT/OFFSET; tree state plus leaf batch |
| Search IDs followed by authoritative HASH fetch | Paged IDs, bounded HASH pipelines | Eligible row-local projection batches |
| General HASH discovery fallback | SCAN pages | Materialized for insertion-order/global-sort and duplicate handling |
| Scalar/group aggregates | Eligible scan input can be paged | Final output can block; cardinality/spill policy depends on aggregate |
| DISTINCT, general sort, windows, joins, correlated/subquery work | Plan-dependent | Existing materialization/caches; no universal memory bound |

Only unordered key-source leaves are added to finite streaming here. Explicit
ordering that needs a global sort and general HASH discovery retain ordinary
execution. Rows/duplicates, residual predicates, pairwise UNION coercion and
per-node limits follow the ordinary path. First-fill errors fail execution;
late errors terminate the reader. Existing context/statement/connection
cancellation, cursor cleanup and worker admission remain shared. This is finite
query streaming, not CDC. `stream_batch_rows` and optional `stream_batch_bytes`
bound output batches, not prepared key lists, retained consumer batches, a huge
row, server sort state, allocator slack or total process memory.

Set statement option `adbc.redis.stream_required=true` to reject materializing
fallback before executing it. This conservative mode accepts unordered
driver-managed table scans/key lookups and eligible unordered UNION ALL trees.
It rejects grouping/aggregates, DISTINCT, ORDER BY, windows, joins, views,
CTEs/derived tables, subqueries, set operations other than UNION ALL and HASH
discovery/global-sort fallback. It requires `stream_results=true`, ExecuteQuery,
one SELECT and at most one bound parameter row. A result small enough for the
first batch can still return whole. Partition publication rejects this option;
partition execution has its own explicit quotas. The default is false and keeps
the ordinary fallback behavior.

This is the blocking-state policy for callers requiring incremental execution:
rejection, rather than an unbounded client-side sort/group/join fallback. It
does not bound parsed SQL/prepared key lists, binding memory, server-side work,
one oversized row or consumer-retained buffers. The general executor remains
plan-dependent: tuple-spill paths use 8 MiB/65,536-row buffers per worker and
32-way merge fan-in, with cancellation cleanup; those are not universal SQL
limits. Group state, ordered-set input, sorting, windows, joins and correlation
caches can grow with cardinality in ordinary mode. For bounded global execution,
use the separately documented retained execution limits, which reject excessive
work before publishing output. Neither mode adds a universal constant-memory
claim. Streaming lifecycle, schemas/duplicates/coercions/limits and repeated
row-count/cardinality measurements are recorded in the closeout evidence.

## Layout assessment and decoded-input limits

The [#208 assessment](https://github.com/redis/redis-adbc-driver/issues/208)
selects the following contracts. Physical encodings are not persisted in HASHes.

| Layout | Decision and validation |
|---|---|
| Map | Reject every key/value type, including NULL containers, duplicate keys, empty and sliced arrays. No key/duplicate/order policy is implied by accepting ordinary lists or structs. Future stored Map semantics: [#267](https://github.com/redis/redis-adbc-driver/issues/267). |
| Dense/sparse Union | Reject both layouts, regardless of type codes, dense offsets or sparse child alignment. A NULL selected child does not make the Union type supported. Future tagged storage: [#268](https://github.com/redis/redis-adbc-driver/issues/268). |
| FixedSizeList | Validate positive width and sufficient child extent, including slices; normalize to List. Preserve parent NULLs and child NULLs; discard fixed-width identity. |
| ListView/LargeListView | Validate offset/size ranges without overflowing their sum. Shared, overlapping and non-monotonic views normalize in logical row order to List, with NULL/empty distinctions intact. |
| Run-end encoding | Accept signed int16/int32/int64 run ends that are positive, strictly increasing, non-NULL and cover the logical slice within the index type's range. Normalize each logical value recursively; encoded NULLs come from the values child. Empty and nonzero-offset slices are supported. |

For any bound batch whose schema contains FixedSizeList, ListView,
LargeListView or run-end encoding (including inside lists, structs or
dictionaries), normalization permits at most **1,048,576 decoded values** and
**67,108,864 decoded string/binary payload bytes** across all its input columns.
Each logical container, scalar and NULL counts as one value; non-NULL containers
also count their decoded children. Run-end and dictionary encodings count their
decoded values. Shared views/repeated runs count every logical occurrence,
including repeated string/binary bytes. A NULL container does not expand its
children. Fixed-size binary bytes count toward the same payload limit.

Structural validation and these checks precede ingest-row allocation and writes
from that batch, and also apply to bound SQL parameters. Exceeding a limit returns
InvalidArgument; split the input into smaller batches. Input buffers, metadata,
Value object overhead, output/IPC buffers, retained batches and aggregate work
are outside the payload counter. These are per-batch decoded-input limits, not
RSS caps or limits on the total rows in a stream. Existing schemas without these
layouts retain their existing limits. Table creation and earlier successful
batches are not rolled back when a later batch rejects.

Canonical normalized output supports IPC streams/files and the existing live
and retained partition paths. Independent readers see canonical schemas, not
input encoding identity. The assessment fixtures compare complete values and
schemas after create/append, fresh-connection metadata reload, IPC, and replay;
retained replay is checked after the live source is truncated. Existing result
layout partition restrictions remain as described above. Map/Union rejection
occurs before table creation and never silently stringifies values.

## Ownership and extension policy

- C Data/Stream import retains the Arrow objects needed while reading. Caller
  ownership follows the ADBC Bind/BindStream contract.
- Parameter conversion and ingestion copy string/binary bytes, including nested
  values, before a bound/imported batch can be released or a stream advanced.
  These are required lifetime copies; removing them creates dangling values.
- C result export shares owned in-process Arrow buffers under release callbacks.
  Consumers retain batches they need after advancing/releasing a stream.
- Redis wire decoding reconstructs typed columns from HASH/Search replies. There
  is no end-to-end zero-copy Redis storage/network path.
- IPC serialization/deserialization and explicit layout conversion can allocate
  additional buffers. Fixed-binary conversion splits source batches to target at
  most 16 MiB of fixed-width payload per output batch (one oversized row can
  exceed this). This excludes source buffers, variable payload, allocator slack
  and retained batches. No reduction in copies or allocation speed is claimed.

### Explicit extension storage normalization

Statement option `adbc.redis.extension_policy` is `reject` by default. Setting it
to `storage` explicitly permits the following **registry revision 1** mappings
for top-level fields, including registered Go/PyArrow types and C-imported
storage fields carrying extension metadata:

| Extension | Required physical storage/metadata | Stored/default output |
|---|---|---|
| `arrow.uuid` | FixedSizeBinary(16), empty serialized metadata; exact 16 bytes, without interpreting a particular UUID version | SQL VARBINARY / Arrow Binary |
| `arrow.json` | String/LargeString/StringView; empty metadata or a JSON object; non-NULL values must be valid UTF-8 JSON | SQL VARCHAR / Arrow String, preserving input text |

This follows [Arrow's canonical definitions](https://arrow.apache.org/docs/format/CanonicalExtensions.html#json).
Names are limited to 256 UTF-8 bytes and serialized extension metadata to 16 KiB
per field. Duplicate/orphan/reserved unknown metadata keys, storage/type
mismatches, conflicting driver SQL metadata, unknown/custom names and nested
extension fields reject. UUID metadata parameters/version changes reject;
JSON metadata may contain future optional fields under its canonical contract.
No executable/plugin loading or dynamic registry installation is driven by
metadata. Driver TIMETZ metadata remains separate.

The selected policy deliberately **normalizes** logical type and metadata: only
canonical SQL storage semantics persist. GetTableSchema, direct queries,
ExecuteSchema, IPC stream/file and independent partition readers report the
canonical types without ARROW extension metadata. There is no UUID SQL type or
generic SQL JSON type added here. To reconstruct an extension, the client must
explicitly wrap the resulting storage array; original extension identity and
metadata cannot be recovered from the table. Appending uses canonical SQL
compatibility rules after extension validation; named Struct schemas must match
even for empty/all-NULL input. Unknown/malformed extensions reject in ordinary
binding, ingestion, schema discovery and partition bindings. Changing the policy
after schema discovery re-checks the original binding schema. Invalid JSON ends
the input reader before writes from that batch; earlier batches and table
creation keep existing autocommit behavior. UUID SQL support remains separate.

## Verification and remaining acceptance

See the [change records](sdlc/changes/arrow-198-209/plan.md) for commands and actual
results. The unit suite, Redis Software 4x QPF focused integration tests and
native C driver-manager tests exercise correctness, schema agreement, ownership
and IPC. These do not establish throughput improvements, general constant-memory
execution, all topology compatibility, or every issue's full acceptance matrix.
See also the [remaining-issues closeout](sdlc/changes/arrow-remaining-closeout/plan.md)
for the completed acceptance matrices, explicit limits/policies and actual
measurements. True Map/Union storage remains in separate implementation issues;
partial matrix classifications describe normalization and allowlist boundaries,
not an assertion that all Arrow physical encodings persist.
