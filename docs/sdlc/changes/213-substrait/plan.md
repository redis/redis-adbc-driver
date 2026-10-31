# Plan and acceptance: Substrait

Date: 2026-10-30. Source: `95d6583d2b07bdfdadc6ddf3b6368bf631139c24`.
Status: design milestone delivered; runtime work pending. Author: Codex.
[Intent](intent.md), [spec](spec.md), [evidence](validation.md),
[issue #213](https://github.com/redis/redis-adbc-driver/issues/213).

## Design milestone

- [x] Review statement/C boundary, metadata, AST, types and scan execution.
- [x] Assess a pinned subset and select an adapter architecture before a library.
- [x] Define extensions, names/schema discovery, parameters and rejection limits.
- [x] Define exact-result, malformed-plan, lifecycle and capability checks.
- [x] Link public unsupported status to the proposed contract.

## Ordered runtime work (not implemented)

1. Confirm a real producer can emit the pinned subset. Check in minimal binary
   fixtures, their readable descriptions, generator/version and exact registry
   signatures. Pin/audit Go messages, descriptor hashes and transitive sources.
2. Implement strict wire/semantic validation and fuzz it before Redis access.
   Cover every nested field, duplicate wire tags and unknown enum/oneof cases.
3. Add typed ordinal vectors and AST translation with an expansion budget.
   Prove NULL/type preservation; extend internal planning where SQL inference
   loses required type/nullability information instead of accepting a mismatch.
4. Add statement query-kind/state transitions, prepare/schema paths and metadata
   revalidation. Add C admission checks using the generation workflow; locate
   the external generator if its template is absent from this checkout.
5. Add scan/byte/deadline accounting to both execution modes and reuse cleanup.
6. Run the acceptance matrix below on Redis Software with active, verified 4x
   QPF (`make -C go redis-up`); retain operational artifacts/credentials privately.
7. Only after full fetch/value validation, enable and verify driver-manager
   feature reporting and update docs/validation configuration together.

## Required acceptance matrix

All success cases compare the plan with equivalent SQL **and independently
specified expected Arrow schema/values**. Drain every reader and check terminal
errors. Compare unordered multisets with multiplicities, since Sort is excluded;
check column order, names, type widths, units, timezone, precision and null bits
exactly. Run both materialized and streamed results and exercise eligible
pushdown versus residual evaluation. SQL agreement alone can hide a shared bug.

| Cases | Required observation |
|---|---|
| Read; Filter; Project direct/emit; Filter then Project | Input fields precede computed fields; reordering/repeated emits and duplicate root names preserve column/value identity. |
| TRUE/FALSE/NULL combinations; typed NULL projections; empty/all-NULL input | Three-valued predicates, matching output nullability, zero rows with correct schema. |
| i64 around 2^53, min/max integers; exact binary/string values | No Search double-rounding loss or TAG normalization errors. Include quotes, dots and SQL-looking identifiers as literal components. |
| Decimal(38) boundaries, negative/scaled/NULL literals | Exact unscaled values, precision/scale; reject bad length, out-of-range digits and invalid precision/scale. |
| Timestamp seconds/ms/us/ns; negative epoch; naive and UTC | Exact units/timezone and values; reject precision outside allowlist and schema mismatches. |
| Identity cast of each supported type; narrowing/mixed types/string-to-number | Identity matches SQL/expected schema; unsupported casts fail before row reads. |
| Float projection incl. NaN, infinities, signed zero | Preserve Arrow values; unsupported float comparisons reject explicitly. |
| Missing/mismatched version; truncated/random wire; unknown nested field/enum; duplicate oneof/tag | Stable InvalidArgument or NotImplemented as specified, no panic or connection poisoning. |
| Unsupported relations at root or nested; files/URIs/Any payloads | Explicit rejection, no external fetch or arbitrary function execution. |
| Extension anchor zero/duplicate/dangling; unknown URI/signature/options; unused unknown declarations | Resolve anchors locally, exact registry/type checks, no fallback by function name. |
| Invalid ordinals/emits, root-name counts, base-schema order/nullability; altered/recreated table | Reject stale/malformed binding before row scan, including constant-false filters. |
| Current/explicit schema, redis catalog, owning pg_temp, adopted table | Same discovery and exact values as SQL; other sessions' temp schemas, views and unregistered HASHes reject. |
| Every plan budget at limit and limit+1; pathological nesting and repeated emits | Bounded allocation/time, deterministic failure, no partial statement mutation. Include C null pointer and oversized size_t without dereferencing/copying. |
| Scan/byte/deadline at boundary; heavily filtered scan | Charge input work too; terminate all modes, never silently truncate successful results. |
| SQL -> plan -> SQL; ingest -> plan; invalid setter after valid SQL/plan; caller mutates byte slice | Atomic replacement, prior query survives failure, no retained caller storage or stale bindings. |
| Bind/BindStream/GetParameterSchema; parameter-like expressions | Empty parameter schema for accepted plan; reject binding/parameters with correct reader release count. |
| Cancellation before decode, during metadata, scan and fetch; timeout; early release; repeated Close | Correct statuses; no leaked buffers, cursors or worker tokens; connection remains usable. Run race and checked allocator tests. |
| Driver-manager GetInfo requested/all codes, unsupported setter, successful subset execution | Current false/null version range; only after implementation true/0.61.0/0.61.0, fully drained metadata/results and correct ownership. |

## Verification commands for the runtime phase

Run focused new validator/translator/state tests, fuzzing with a bounded corpus
and `go test -race` for relevant packages. Then use the repository unit pass
`cd go && env -u REDIS_URI go test -count=1 -p 1 ./...` and shared-library build
`make -C go build`. On the verified dedicated Software environment run
`REDIS_URI=redis://localhost:6379/0 make -C go test` and `make -C go validate`
with the same URI. Add explicit Python driver-manager plan/GetInfo tests if the
upstream validation suite lacks them. Record skips; skipped integration does
not satisfy this gate. No runtime commands are claimed as run for this milestone.
