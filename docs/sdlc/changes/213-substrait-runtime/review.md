# Review: bounded Substrait runtime

Date: 2026-11-04. Author: Codex. This is an agent review against
[REVIEW.md](../../../../REVIEW.md), not maintainer approval.
[Specification](spec.md), [validation](validation.md).

## Correctness and lifecycle

Reviewed ordinal vectors, Project direct/emit ordering, duplicated labels,
NULL/type derivation, exact decimal/timestamp literals, comparison restrictions
and metadata revalidation. Plan text is never parsed as SQL. Required output
nullability is independent of SQL's default nullable result schema.

The owned reader shares scan/evaluation/Arrow conversion with SQL while keeping
its typed schema, copied options, deadline and work counters. Both result modes
consume it. SQL/plan/ingest transitions retain prior state on failed setters and
release bindings once on success. C SQL diagnostic state clears only after a
successful plan setter. Early release, Cancel, Close and EOF share cleanup;
caller-retained record batches stay valid. Cancellation uses execution-owned
transports, including during metadata/scan admission.

## Security and bounds

Reviewed strict recursive wire traversal before protobuf allocation: unknown
fields, duplicate singular/oneof fields, unknown enums, integer overflow,
collection/payload/depth/node ceilings and packed emit counts. Semantic validation
rejects unsupported operators, type variations and extension declarations even
when unused. Extensions resolve through the two offline pinned registries;
there is no URI fetch, SQL function dispatch or external execution engine.

The C guard rejects null/nonzero pointers and oversized lengths before copy or
integer narrowing. A native harness tests invalid addresses and SIZE_MAX,
then executes prior SQL to prove ordinary rejection does not poison the driver.
Scan budgets charge input before residual filtering; HASH discovery also bounds
retained keys and work. Arrow output charges its values before batching.

## Corrections made during review

- buf managed mode initially rewrote other languages' descriptor options. Go
  import mappings now preserve every upstream descriptor option; a test compares
  generated descriptors with the hashed pinned descriptor set.
- Wire count/payload admission moved ahead of protobuf allocation. Overflowing
  int32/uint32/enum values cannot truncate into valid identifiers or ordinals.
- Scan accounting is a nil pointer for ordinary SQL, avoiding a context lookup
  per SQL row and supporting context-free internal scan fixtures.
- Substrait error boundaries preserve ADBC status codes while suppressing
  Redis command/index/key details; validator budget/path messages remain stable.
- Reader options/allocator are captured instead of consulting mutable statement
  options after execution. IPC input/output reject explicitly.

No unresolved actionable finding was identified in this review. Limits remain:
local macOS ARM64/Software evidence only; no hosted CI, other platforms, external
SQL-to-Substrait producer interoperability, release or performance claim. These
are evidence boundaries, not implied passing checks. See validation for actual
success/failure/skip records and the original acceptance matrix for scope.
