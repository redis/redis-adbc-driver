# Specification

Date: 2026-11-19. Status: scoped implementation design; no completion claim.
See [intent](intent.md), [plan](plan.md), and [validation](validation.md).

## Constraint declarations

Persist named/unnamed UNIQUE and FOREIGN KEY declarations and ordered stable
column identities. Expose exact GetObjects constraint arrays and foreign usage
under the pinned catalog definition. Track resolved referenced table identity so
rename/rekey follows that object and name reuse does not bind a replacement.
Retain truthful unresolved declarations without silently inventing referenced
usage. Preserve historically accepted unenforced forms; metadata publication does
not add uniqueness, referential-integrity or ON CONFLICT guarantees. Existing
CHECK/PRIMARY KEY names/lifecycles and metadata-slot publication remain correct.

## Table-type filters

Exercise native GetObjects for TABLE, VIEW, MATERIALIZED VIEW and omitted/empty,
single/multiple and no-match filters, combined with other discovery filters.
Enable the validation flag if existing behavior satisfies the contract; fix only
reproduced gaps. Preserve discovery permissions and exact metadata schemas.

## Real catalog namespaces

Introduce an explicit persisted logical catalog registry with independent schema
and relation identities. Preserve the existing redis catalog's physical layout.
Nondefault catalog/schema pairs may use a reserved, injective internal storage
identity; that identity must never be exposed as a user schema/catalog alias.
Provide a usable catalog lifecycle. Reject user names that enter internal reserved
namespaces. Resolve catalog/schema/table consistently for SQL names, metadata,
current catalog/current_database and explicit Arrow destination options.

Catalog selection is connection-local. Unknown/unauthorized requests preserve the
prior setting and refuse before input consumption or mutation. Same-name tables
in distinct catalogs stay independent. Explicit Arrow destinations may differ
from the connection's current catalog. Existing ownership, publication, no-replay,
stream ownership and temporary-object boundaries remain authoritative. Unsupported
named-catalog live/foreign/temp/shared-view/mixed write shapes must fail clearly.
Do not weaken storage protocol, immutable root/pin/GC or reader permissions.

## Acceptance

Run all seven named upstream tests without capability skips through a correctly
built native library on Redis Software with verified active/effective 4x QPF.
Add focused isolation, constraint lifecycle, unknown-target, cancellation and
filter regressions. Retain transaction skips and explicit enforcement limits.
Complete source/unit/build and affected integration checks; create a reviewable
PR and follow its checks. No merge or new release is assumed by this change.
