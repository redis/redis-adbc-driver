# Specification: existing Redis ADBC contracts

Date: 2026-10-20. Status: **retrospective draft, not original design approval**.
Source: `6f42461`. Inputs: [intent](intent.md), [context](context.md).
Follow-up: [plan](plan.md). Maintainer acceptance: not recorded.

## Functional requirements and implementation

| Contract | Implementation boundary | Evidence to use for a change |
|---|---|---|
| ADBC handle lifecycle, options, metadata and parameter binding | `go/pkg/`, driver/database/connection/statement implementations | Go handle tests, Python ADBC validation and catalog reflection |
| SQL queries, DDL/DML, expressions and documented relational forms | `go/sql.go`, query/evaluator and feature-specific files | Focused SQL regression and integration cases; pgdiff for PostgreSQL claims |
| Typed Arrow ingestion and result delivery | `go/ingest.go`, `arrowconv.go`, `stream.go` | Bind/ingest/stream tests including NULLs, type widths, buffer lifetime and early close |
| Search acceleration with exact fallback | Store/search, aggregate and query execution | Same expected results under eligible pushdown and fallback; exactness edge cases |
| Managed tables and eligible external HASH collections | `go/store.go`, `adopt.go`, `rekey.go`, CLI | Copy/adopt/drop/rename/recovery tests with ownership checks |
| Standalone, Software proxy and OSS Cluster API paths | `go/database.go` and storage key construction | Relevant matrix jobs and cluster integration cases |
| Shared worker admission and automatic allocation | Worker budget/tuner, parallel aggregate execution | Cancellation/race tests and controlled measurements with complete coverage |

## Data and execution design

Row HASHes hold authoritative text-encoded values; NULL/missing defaults and
hidden identifiers follow `store.go` and existing default-handling rules.
Search indexes are derived structures. A plan may push filters, ordering and
reductions only when index representation and server capabilities preserve
semantics; otherwise the driver reads/evaluates values itself.

Metadata uses the `{meta}` hash slot. Physical row/index prefixes have generation
history and are not reused after replacement. Connection-open recovery means
independent test packages must not race fixtures on a shared database.

Stream ownership lasts beyond execute. Fetch errors, cancellation and early close
must terminate work and reclaim resources. Diagnostics report execution stages;
`execute_returned` alone does not establish complete fetch or correct output.

## Compatibility and operational constraints

Search is probed on connection. Flex is explicitly refused. Transaction methods
do not provide multi-statement transactions. Keep validation feature declarations
aligned with implementation; accepted UNIQUE/FOREIGN KEY syntax does not imply
constraint enforcement. SQLSTATEs and physical Arrow types are observable API.

The module currently declares Go 1.26.6; Python validation needs >=3.13. Shared
library builds require cgo. The adoption CLI builds without cgo. Current release
automation covers Linux amd64/arm64 and macOS arm64; local host build capability
is broader than this published platform matrix.

## Acceptance criteria for future modifications

1. State input SQL/Arrow schema/options and expected rows, output schema, ordering
   or SQLSTATE. Include NULLs and boundary values relevant to the change.
2. Identify affected execution paths and prove equivalent behavior where paths
   can differ. Preserve externally owned data and recovery invariants.
3. Run applicable [validation checks](validation.md); record source revision,
   environment, exit results and skips. Existing dated pass counts are context.
4. For an optimization, preserve independent result validation and report the
   complete measurement protocol, resource use and uncertainty. An incomplete
   readiness gate is inconclusive.
5. Update public docs/options and the change's plan together with behavior.

## Risks and unresolved concerns

The curated PostgreSQL corpus contains known differences. TAG normalization,
floating reductions, precision and cluster behavior can invalidate pushdown.
Recovery, ownership, Arrow lifetime and cancellation bugs can corrupt data or
leak work. Diagnostic plans may expose SQL literals. Validate these areas using
[REVIEW.md](../../REVIEW.md), not only happy-path examples.

Named approvers, production SLOs and deployment-specific policy are unknown.
There is no inference here that historical work passed an unrecorded gate.
