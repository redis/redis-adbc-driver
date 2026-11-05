# Bounded Substrait plan support

Date: 2026-11-04. Implementation baseline: `f4d5b787e8c28c036baf49de9190c4e6f83ca2e8`.
Status: bounded runtime implementation for [issue #213](https://github.com/redis/redis-adbc-driver/issues/213).
Author: Codex, requested by the repository user. Maintainer acceptance is not
recorded. [Intent](sdlc/changes/213-substrait/intent.md),
[plan](sdlc/changes/213-substrait/plan.md),
[evidence](sdlc/changes/213-substrait/validation.md).

`SetSubstraitPlan` accepts binary **Substrait 0.108.0** plans in the subset
below. `InfoVendorSubstrait` is true and min/max versions both report `0.108.0`.
This does not claim full Substrait coverage. See the
[runtime validation record](sdlc/changes/213-substrait-runtime/validation.md).
The earlier design records above remain historical evidence.

## Feasibility decision

The supported grammar reads one base table, optionally filters it, and
optionally projects fields and typed expressions. Validation establishes the
relation shape, exact types and schema binding before execution.

| Existing boundary | Reuse and necessary restriction |
|---|---|
| [`statement.go`](../go/statement.go): prepare, binding, execution and schema | A separate plan state bypasses SQL parsing and revalidates table metadata. |
| [`sql.go`](../go/sql.go): `SelectStmt`, `ColumnRef`, `Literal`, `Binary` | Construct a validated AST with typed literals and canonical column names; keep positional Substrait fields in a separate ordered vector. |
| [`exec.go`](../go/exec.go): `resolveTable` | Reuse metadata and planning after enforcing base-table-only binding. Ordinary SQL planning also accepts views and functions, which this subset excludes. |
| [`query.go`](../go/query.go): `planWhere`, `prepareScan`, scan iterators | Preserve exact HASH values and residual predicates. Passing a predicate to Search does not prove exact evaluation. |
| [`types.go`](../go/types.go), [`arrowconv.go`](../go/arrowconv.go) | Reuse physical Arrow mappings only for exact logical matches; prohibit implicit coercion and precision loss. |
| [`stream.go`](../go/stream.go) | Reuse stream ownership/cancellation with a typed Substrait reader; both result modes share its admission limits. |
| [`pkg/driver.go`](../go/pkg/driver.go): C entry point | Validate size and pointer before the C-to-Go copy; a Go-only size check comes too late. This file is generated: update its generator/source workflow too. |

The driver uses a strict wire validator, pinned generated protobuf messages,
and a small driver-owned typed AST adapter. The existing scan iterators,
expression evaluator and Arrow builders execute it. No SQL text is constructed
from plan identifiers. [Schema sources, hashes, licensing and regeneration](../go/internal/substrait/README.md)
are checked in. The upstream Go module release is tracked by
[substrait-protobuf#7](https://github.com/substrait-io/substrait-protobuf/issues/7).

## Wire format and relation grammar

Protocol pin: **Substrait 0.108.0**, binary `substrait.Plan` only. The version
range is deliberately exact.
Require an explicit matching version and execution behavior
`VARIABLE_EVALUATION_MODE_PER_PLAN`; reject other behavior, versions and nonempty
`git_hash`. Producer text is diagnostic only. JSON, text protobuf and compressed
payloads are outside the API. Pin the complete transitive descriptor set from
[v0.108.0](https://github.com/substrait-io/substrait/tree/v0.108.0/proto/substrait),
including [plan.proto](https://github.com/substrait-io/substrait/blob/v0.108.0/proto/substrait/plan.proto).

Exactly one root and this input-to-output grammar are candidates:

```text
Read(named_table, base_schema) -> [Filter(condition)] -> [Project(expressions)] -> Root(names)
```

Each bracket means zero or one operator. No filter above a project, repeated
projects, relation references, joins, aggregates, sorts, fetch/limit, writes,
virtual tables, files, exchanges or extension relations. Reject all fields
outside the allowlist, including unknown protobuf fields, advanced extensions,
hints, expected type URLs, read masks and read filters/best-effort filters.
Putting filtering in `Filter` alone avoids multiple initial semantics.

Read and Filter permit absent/direct `RelCommon` only. Project permits direct
or emit: its ordered vector is input fields followed by newly computed fields;
emit selects/reorders that vector and may repeat an index. Validate every index.
Expressions reference the Project input only. Translate selected vector entries
to explicit SELECT items; never implement Project as replacement by default.
Require nonempty final output and exactly one Root name per output column;
preserve names and duplicate names without treating them as SQL identifiers.
These choices follow the pinned
[algebra definitions](https://github.com/substrait-io/substrait/blob/v0.108.0/proto/substrait/algebra.proto).

## Table binding and schema discovery

Named-table components mean `[table]`, `[schema, table]` or
`[redis, schema, table]`. Components are literal identifiers, not dotted SQL,
URIs, HASH prefixes, index names or Redis commands. Reject empty components,
invalid UTF-8/NUL and other catalogs. Use `resolveTable` for current-schema and
connection-owned `pg_temp` rules; never access another session's physical temp
schema. Then require stored base-table metadata, including eligible adopted
tables; reject views and inferred/unregistered HASH collections.

Require a flat base schema matching the table's full ordered public columns:
names, logical types, precision/scale, units, timezone and nullability. No hidden
row identifiers, missing columns, subsets or implicit widening. Resolve field
references by zero-based ordinal against this checked vector, using canonical
names only after validation. Allow root-relative direct struct-field references
with no child; reject outer, expression-rooted and nested references.

`GetTableSchema` supplies discovery before plan generation. `Prepare` validates
and binds metadata without scanning rows. `ExecuteSchema` returns the derived
output schema without scanning. Recheck the base schema at each execution/schema
request so altered or recreated tables cannot reuse stale ordinal bindings.
This does not add snapshot isolation: existing autocommit concurrency limits
still apply. Never retain a planned physical row prefix across executions.

## Types, functions and parameters

All types require explicit required/nullable declarations and the default type
variation. Reject user-defined types/variations even when declared but unused.
The initial candidates map as follows; unsupported table types reject the whole
plan even when projected away.

| Substrait candidate | Driver representation / restriction |
|---|---|
| bool, i16/i32/i64, fp32/fp64 | Same-width `ColType`; i8 is excluded because there is no matching driver Kind. Floating values may be read/projected, but floating comparisons are excluded pending NaN/ordering proof. |
| string, binary | Unbounded text/binary with exact bytes; reject fixed/length-constrained strings, aliases and nondefault collations. |
| decimal | Decimal128, precision 1–38, scale 0–precision; validate the signed 16-byte little-endian literal and digit range without float conversion. |
| precision_timestamp / precision_timestamp_tz | Precision 0/3/6/9 only, matching seconds/ms/us/ns; timezone is respectively naive/UTC. No session-local conversion, rounding or unit truncation. |
| typed NULL | Retain the declared type through planning and Arrow output; do not let SQL's unknown-literal inference choose it. |

Exclude date/time/interval, containers, UUID and deprecated timestamp encodings
initially. See pinned
[type.proto](https://github.com/substrait-io/substrait/blob/v0.108.0/proto/substrait/type.proto)
and the literal definitions in algebra.proto. Decimal and timestamp eligibility
requires the differential cases below; adding a mapping alone is insufficient.

Extension identifiers are offline lookup keys only; no plan-supplied URI or
URN is fetched. Only `extension:io.substrait:functions_comparison` and
`extension:io.substrait:functions_boolean` are accepted. Their exact definitions
are pinned from Substrait v0.108.0 in the binding record. Resolve URN anchor ->
function anchor -> full declared signature -> typed arguments/output. Anchor
zero is valid. Duplicate/dangling anchors, unknown declarations (even unused),
options and mismatched output types/nullability are rejected.

Accepted signatures are `equal:any_any`, `not_equal:any_any`, `lt:any_any`,
`lte:any_any`, `gt:any_any`, `gte:any_any`, `is_null:any`, `is_not_null:any`,
`and:bool`, `or:bool`, and `not:bool`. The variadic boolean signature includes
its argument type once; this subset accepts exactly two arguments for and/or.
Comparisons require identical supported operand types; boolean comparisons
allow equality/inequality only. Floating comparisons reject; null tests and
projection allow floats. No mixed-type coercion or arbitrary SQL function
dispatch occurs. Boolean operations use three-valued logic; filters retain
only TRUE. Null tests return required boolean; comparison nullability mirrors
inputs, and boolean function output is nullable per its pinned overload.

Only identity casts with matching type attributes and nullability are candidates;
require explicit throw-on-failure behavior. Reject unspecified/return-null cast
behavior, narrowing, string/numeric conversion and timezone conversion. SQL
supports more casts, but that is not proof of identical Substrait semantics.

Parameters are excluded in the first subset: reject every parameter expression.
For an accepted plan `GetParameterSchema` returns an empty schema; `Bind` and
`BindStream` return `StatusNotImplemented` without retaining new input. Preserve
the existing BindStream ownership contract when rejecting an owned reader.
Do not reinterpret bound batches as ingestion or silently repeat execution.

## Resource, error and lifecycle contract

These are hard ceilings for Substrait execution. Check before allocating
or expanding data, and at each traversal loop:

| Budget | Maximum |
|---|---|
| Serialized plan, checked at C boundary before copying and in native Go | 1 MiB |
| Protobuf message nesting / semantic expression depth | 64 / 32 (both apply) |
| Relations / expression nodes (whole plan) | 3 / 4096 |
| Input columns / output columns / project expressions | 256 / 256 / 256 |
| Extension URNs / declarations | 2 / 64 |
| One literal payload / identifier component | 64 KiB / 1024 UTF-8 bytes |
| Total translated nodes, including emit duplication | 8192 |

Use strict wire traversal plus the pinned protobuf runtime recursion limit
and a bounded semantic validator; do not discard unknown fields. Inspect nested messages too. Reject
duplicate singular/oneof wire fields rather than accepting last-value-wins
ambiguities. Bounds must cover discarded/unreferenced fields and declarations.
For the C wrapper reject null pointers with nonzero length and values above the
budget before any narrowing to `C.int` or `C.GoBytes`. Never panic/poison the
driver for an ordinary invalid plan.

Plan limits alone do not bound a table scan. The execution path uses shared accounting for both materialized and streamed paths: at most 1,000,000
decoded rows scanned (including filtered-out rows), 256 MiB cumulative decoded
input/output payload, and a 30-second deadline capped by any earlier caller
deadline. Check accounting before buffering rows; use overflow-safe counters.
Exceeding a budget fails the query rather than truncating results. Streaming
may already have delivered batches; surface a terminal reader error and clean
up. Ordinary SQL keeps its existing limits. Accounting conservatively charges raw
input payload and fixed per-value overhead before decoding, plus output and
retained HASH-discovery keys. Redis reply pages and Arrow batches are bounded
independently; the byte counter is not a process-RSS measurement. HASH discovery
also has a 1,000,000-key-visit work limit, including duplicates.

| Condition | ADBC status |
|---|---|
| Closed statement / no executable query | InvalidState |
| Malformed wire, missing required structure, bad ordinal/type/schema, duplicate anchor | InvalidArgument |
| Version, relation, extension, cast or parameter outside the subset | NotImplemented |
| Missing table/catalog | NotFound |
| Size/depth/node/scan/byte budget exhausted | InvalidArgument, with stable budget name and no plan payload |
| Caller cancellation / deadline (including internal deadline) | Cancelled / Timeout |

Validate into temporary owned state. A failed setter preserves the prior query,
bindings and ingest mode; a successful setter atomically replaces them and
releases prior parameters once. Copy/own caller bytes; mutation after return
must have no effect. `SetSqlQuery` must clear plan state, and ingest selection
must clear both query kinds. Check cancellation during decoding/translation and
metadata lookup; no Redis row reads until the whole plan is validated. Release
readers, cursors, worker tokens and any new budget state on failure, Cancel,
Close and early reader release. Diagnostics identify a structural path/status;
do not expose literals, raw plans, URIs with credentials or physical keys.

## Capability reporting and execution boundaries

`GetInfo` reports true and min=max `0.108.0`. Driver-manager tests cover both
requested-code and all-code queries and completely drain metadata/results.
A populated C function pointer alone is not evidence of execution support.

Prepare and ExecuteSchema revalidate metadata without scanning rows. A failed
setter preserves prior SQL/plan/bindings/ingest selection. Successful query-kind
changes clear bindings exactly once. Accepted plans have an empty parameter
schema; Bind/BindStream reject with NotImplemented and preserve ownership.
Partitions, CUDA, IPC and alternate result layouts are outside this subset and
reject explicitly. Setting SQL or ingest mode clears the plan. Close releases
active Substrait readers; caller-retained Arrow buffers remain valid.

The [original acceptance matrix](sdlc/changes/213-substrait/plan.md) defines the
broader design gates. The [runtime record](sdlc/changes/213-substrait-runtime/validation.md)
states the checks actually run and remaining gaps. Full Substrait coverage is
outside this bounded implementation.
