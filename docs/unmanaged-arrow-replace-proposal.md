# Proposed unmanaged Arrow replacement contract

Status: **proposal for review; unsupported at runtime**. Requested by Albert Wong
on 2026-11-17 in [#399](https://github.com/redis/redis-adbc-driver/issues/399).
The requested deliverable is the contract, not implementation. Existing append,
create and create_append behavior and managed atomic replacement remain unchanged.
See [unmanaged tables](unmanaged-tables.md) and the
[change record](sdlc/changes/issues-399-444-447/intent.md).

## Ownership and admission

Replacement would require a new explicit destructive-operation opt-in, naming the
exact registered table, application prefix and index. Registration, a matching
prefix, or index membership alone never establishes exclusive ownership. Proposed
first-version scope is an administratively dedicated prefix and dedicated index;
shared prefixes, overlapping/nested prefixes, multi-prefix or filtered indexes,
foreign index registrations, and unmodeled index attributes are refused. Checking
only existing indexes cannot discover every external consumer: an administrator
must attest exclusive use and quiesce all application writers for the operation.
A driver lease coordinates driver clients only and cannot fence raw Redis writes.
If exclusive use cannot be established, replacement stays unsupported; use a new
prefix/index and perform an application-controlled cutover instead.

Resolve and pin registration, authenticate the actual principal, validate exact
index definition and explicit scope, and preflight required commands before
calling Next on Arrow input or mutating any source. Reject missing/changed
registration and incompatible schema/index configuration at this stage. Do not
infer permission from a prior connection or cached successful write. Preflight
cannot guarantee future permission availability; later failures remain partial.
Concurrent DROP/recreate must invalidate admission rather than retarget a new
table with the same logical name. No automatic retry or input replay is allowed.

## Data, schema and index scope

A successful replacement would leave exactly the new input's public schema and
rows. Every input row must supply a unique, non-NULL string __key inside the
exact dedicated prefix. Treat Redis keys as byte strings: no path normalization
or glob-pattern interpretation. Reject metadata namespace keys and keys in
excluded child scopes. Ambiguous scope is a refusal, not permission to delete.
Duplicate keys and conversion failures discovered during input are errors; input
is streamed once and need not be rewindable.

Old HASHes absent from the new input must be deleted. Replaced HASHes are complete
images: old unmodeled fields are removed. Proposed TTL policy is explicit loss of
old TTLs for replaced keys (new rows are persistent), including keys present in
both old and new input. No TTL preservation is implied. Non-HASH keys in the
prefix are an admission refusal, and a type change during execution is an error.
Nested/overlapping application collections are outside the supported scope.

Only the explicitly dedicated index could be replaced, and only after validating
that its definition is representable by the new Arrow schema. Never mutate any
other index or create hidden managed fields. Schema/index replacement affects
all consumers of that index; this is part of the administrator's exclusive-use
attestation. There is no distributed atomic commit across index DDL, HASHes and
the driver's registration metadata. A failure may leave mismatched schema/index
and data; no successful operation is reported until all three agree.

## Execution, concurrency and failure

Proposed implementation is a bounded direct-source operation, with deterministic
key enumeration and bounded complete-image writes/deletes. Design must establish
an authoritative inventory without persisting Redis SCAN cursors, unbounded
client key retention or assuming Search covers all HASHes. Index membership alone
cannot determine deletion scope. This inventory and the index/schema transition
are unresolved implementation gates; this document selects no storage protocol.

Application writes must remain quiescent from inventory through completion. A
concurrent raw writer can insert after enumeration, overwrite new images, change
TTLs, or be overwritten/deleted. The driver cannot promise conflict detection
for arbitrary raw Redis clients. Detection of a changed definition, registration,
or observed key stops the operation; it never broadens scope or replays a batch.
A practical implementation must fail closed unless the exclusive-use contract is
explicitly accepted. Online shared-prefix replacement is excluded.

Readers may see old rows, missing rows, new rows and temporary index/schema
mismatch. No whole-operation atomicity, rollback, statement snapshot of source
HASHes, or process resume is promised. Managed replacement instead atomically
publishes a complete managed generation and does not mutate import sources.

On cancellation, Close, conversion/reader error, permission loss or connection
failure, stop consuming input and issuing new mutations. Preserve original ADBC
status and SQLSTATE. Report separate acknowledged delete and write counts plus
the current mutation's known/unknown outcome and the failed phase. Lost replies
make acknowledged counts lower bounds; never infer zero work from zero replies.
Do not replay an uncertain mutation, reconsume input or claim the previous data
was restored. Keep diagnostics free of credentials and sensitive source values.

Recovery is operator-owned: inspect all affected source keys, index definition,
registration and TTLs against a trusted backup/input manifest, reconcile them,
then start a separately authorized operation with fresh input if appropriate.
DROP and GC continue to preserve application-owned resources, including remnants
of a failed replacement; no source prefix is passed to managed reclamation.

## Review and implementation gates

Before runtime support, settle the destructive opt-in API, exclusive-use
attestation, authoritative bounded inventory, schema/index transition ordering,
and progress/error representation. Review this proposal explicitly; its existence
is not approval. Keep current early NotImplemented refusal until then.

Required verified Redis Software 4× QPF tests cover dedicated/shared/nested scope,
filtered/multi-prefix indexes, outside and binary keys, duplicate/NULL keys,
schema/index/TTL/unmodeled fields, raw-writer interference, DDL races, actual ACL
denials, cancellation/Close, reader/conversion errors, reply loss before/after
acknowledgment, and independent complete final-state inspection after every
partial failure. Retain managed replacement, unmanaged append/create and DROP/GC
ownership regressions. A proposal-only change does not claim these tests passed.
