# Reader authorization integration

This is staged production work for the required SQL SELECT gate in PR #284.
The gate remains required; the first integration checkpoint still refuses SQL
GRANT/REVOKE until generation-stable table authorization is integrated. A passing
subtest below does not establish complete authorization parity or CI success.

## Import payload boundary

At source `95643ed`, the Software RBAC fixture reproduced the hosted connection
failure: restricted Open attempted catalog initialization and was denied EVAL.
An independent production-capture regression appended an actual encoded row via
`newSnapshotImportSpool`, resolved its reference-owned physical journal and
verified owner decoding. Restricted HGET, HMGET and HGETALL all exposed the
captured value with the original broad metadata wildcard. Commit `2e9769e`
replaces that wildcard with positive metadata families. The regression passes;
RBAC unit tests pass. Journals retain their existing ownership and GC lifecycle.

## Reader-control checkpoint

The integration uses format 16 at the original catalog anchor, and a versioned
workspace/reference-graph/queue namespace. Existing storage is refused without
migration. A frozen protocol-15 row collector is executed against current keys
and refuses admission before mutation. The existing exact-prefix alias gate
also passes.

Restricted Open inspects command capability without writing. It neither
initializes the catalog nor starts writer recovery, temporary cleanup, or
compaction. Each principal has its own lease HASH; EVAL key envelopes are
separate ACL selectors from permitted inner commands. Readers can register
their namespace but cannot remove another namespace, write catalog fields,
write rows, or write another principal's HASH. Untrusted lease JSON is checked
for bounded expiry and a committed root before it protects anything.

Admission and collection validate an exact declared-key directory snapshot
before mutation. Only a proven stale-directory refusal is retried; transport
errors are not replayed. Point validity/renew/release avoid global enumeration.
Both catalog and principal pins count toward the 128-pin bound. There are at
most 128 principal namespaces and 128 fields per HASH. Oversized or malformed
directories fail closed and require administrator reconciliation; these ACLs
do not prevent a malicious principal from causing denial of service. Collectors
prune expired fields and empty namespaces. Shared-role and multiple-role user
configurations are refused by SQL grant management rather than broadening lease
write permissions.

On an isolated Redis Software RAM fixture, with effective 4x QPF, noeviction and
strict data-plane TLS, these production regressions pass:

- Restricted Open and initial ungranted SELECT denial.
- Captured payload direct reads and scripted reads denied.
- Direct/scripted catalog writes, row writes, peer lease writes and directory
  removal denied.
- A restricted pin keeps actual dropped-table rows alive through collection;
  release allows reclamation.
- A cross-principal live partition worker survives producer release. Late
  acquisition after producer release fails, and active worker release permits GC.
- 128-reader admission, overflow refusal, expired and wrong-root renewal
  refusal, no silent repinning of an existing expired token, quota recovery and
  empty namespace reclamation.
- Existing point-lease tests, partition generation/release/expiry/tampering,
  and sequence reset/volatility/GC/lost-reply regressions.

These results are from local macOS ARM64 Go tests against Software, not hosted
AMD64/ARM64 native completion. The whole SQL SELECT test still fails at the
explicit GRANT barrier at this checkpoint. Remaining gates include stable
table access identity through all DML/DDL/copy/refresh paths, actual granted
SELECT and REVOKE, two restricted principals, stale-directory and uncertain
reply injection, retained results, the complete race/native suites, resource
measurements and replacement hosted CI. Raw logs and fixture configuration are
private; the owned fixture remains available for those checks.

## Stable-access checkpoint

The next local checkpoint passes the entire required `TestSQLRBACSelect`, with
all ordinary Go packages also passing. It adds a separate `SnapshotAccessID`
ahead of schema and generation components in row keys; DROP/recreate allocates
a new identity. DML, TRUNCATE, ALTER rewrite, compaction, Arrow replacement and
managed-copy refresh preserve that identity. Replacement/refresh also preserve
the table OID used by the grant ledger. Search indexes have a versioned name so
old orphan workers cannot mistake them for abandoned protocol-15 indexes.

RBAC's administrative lock and grant ledger are deliberately outside immutable
SQL roots. Ambiguous API writes retain the shared lock across SQL pin/root
lifetimes; successful lock removal compares its owner token. These management
operations do not promise a transaction with SQL publication.

Additional actual Software checks pass: granted reads across INSERT, UPDATE,
DELETE, ADD/DROP COLUMN, type conversion and compaction; two restricted users in
different dedicated roles deny each other's direct and scripted lease writes;
both deny captured import data; cross-role partition workers remain valid after
producer release. Production fault injection proves a stale-directory refusal
creates no partial pin and permits one safe retry. A lost acquire reply is
resolved using read-only lookup of the original token after another root is
published, with exactly one acquire execution.

Untrusted directories and HASHes have byte checks before materialization:
atomic SCARD/MEMORY USAGE SAMPLES 0 checks precede SMEMBERS, and each HASH is
limited to 128 fields and 256 KiB before HGETALL. Lease payloads are at most
1024 bytes before decoding. Malformed names, 129 names, a 300 KB directory
member, 129 HASH fields and a 300 KB HASH value all cause collection to refuse
without freeing a live root. Explicit fixture-owner repair restores collection.
Raw ACL commands can still poison these structures: these are fail-closed
protocol resource limits, not backend-enforced availability guarantees.

**Shared-role compatibility is still open at this intermediate checkpoint.**
The per-user namespace variant precisely refuses a second same-role user at
Open and rejects grant management before mutation. Backend ACLs are shared by
role, however, so per-user raw isolation cannot be promised within a shared
role. The planned follow-up uses a namespace per backend role/ACL trust domain
to preserve the prior shared-SQL-role contract while keeping different-role,
catalog, row and payload boundaries. Full snapshot/native/race and hosted
validation remain pending; this checkpoint alone is not a completed CI repair.

## Shared backend role domains

The follow-up restores shared-role compatibility: two actual Software users in
one role both Open and SELECT through the role's domain. Different-role direct
and scripted lease write denials remain tested. Users with additive multiple
role memberships still receive the preexisting unsupported-mapping refusal.
The explicitly configured grant administrator validates bindings and registers
domain identities; restricted Open discovers its one permitted domain using
nonmutating capability checks over explicitly declared keys. Readers cannot
write the domain registry. Same-role users intentionally share their backend
ACL rights; this does not promise per-user raw isolation within that role.

The complete SQL RBAC fixture and ordinary Go packages pass at this checkpoint.
Retry classification now requires a direct native RedisError with exactly the
fixed pre-mutation refusal. Plain lookalikes, wrapped native errors, joined
EOF/native errors and prefixed messages are rejected. Actual cancellation after
a proven refusal stops before a second admission command.

`redis-adbc reader-acl` prints the driver's canonical policy without connecting
to a server. The independent native fixture uses that policy and stable table
access prefixes instead of broad metadata/schema wildcards. Its metadata
permission probes now target snapshot directories and actual physical objects.
These native fixture changes still require live validation, along with the
complete snapshot/race selection and hosted replacement workflows.

## Global management lock and resource sample

The moved administrative SET NX now carries the transport NoRetry marker. An
unknown successful lock reply stops before management API writes and leaves the
original lock owner for reconciliation. The production regression fails against
the preceding implementation (NoRetry=false, one observed mutation), then passes
with the once command, retained original token and unchanged API write count.
The complete Software SQL authorization fixture also passes under `-race`.

At 128 legitimate pins in one role domain, Software reports 20,280 bytes for the
lease HASH and 130 bytes for the one-member active directory (MEMORY USAGE,
SAMPLES 0). The reader control script is 5,574 bytes. These are metadata samples
on this fixture, not total server RSS, per-query peak allocation or throughput
benchmarks. The enforced per-HASH/directory limit remains 256 KiB.

The rebased broad race run completed import, compaction, ownership and metadata
lifecycle cases without race reports, then stopped on upstream telemetry-wrapper
test assumptions in expiry/Substrait cases. Those cases pass with the workflow's
`REDIS_ADBC_TELEMETRY_DISABLED=true` setting. Enabled-wrapper compatibility is
being repaired separately in the upstream reconciliation; neither this partial
run nor the CI-env selections establish a complete enabled-accounting race run.

## Native metadata follow-up

The independent native run at reader source `8ae4098` passed 11 checks including
the previously failing restricted cross-process full-value/schema replay,
explicit layouts, direct HASH reads, certificate failures and write denials.
It then failed metadata discovery: missing view directories resolve to empty
`absent:*` addresses, and column discovery unnecessarily opened row indexes.

The follow-up permits only that empty-address read family and makes schema/
column discovery read immutable metadata with descriptor validation. Row queries
retain their index/reference checks and row ACLs. An actual restricted metadata
regression runs before GRANT; row SELECT remains denied and no management ACL
write occurs. DROP/recreate also receives a fresh access identity and cannot
inherit the dropped table's grant. The full SQL authorization race fixture and
the sequence, live partition and frozen-format selections pass with actual
Software fixture URIs and no skips.

Administrative partition release now reaches restricted producers' groups while
keeping active workers alive. Restricted callers still release only their own
backend domain. Partition producer membership and sequence/group lease values
are checked before decoding, including the 1024-byte limit. The current reader
script sample is 5,831 bytes; the 128-pin HASH/directory samples remain
20,280/130 bytes. Native cleanup removes only its known generated schema,
including leftover fixture tables after failures. Full native completion and
the final combined broad suites remain pending at this checkpoint.

## Combined reader and direct-partition checkpoint

Reader source through `0b21b18`, combined with the upstream telemetry, hidden-cap
and direct-partition changes through `b1bc95b`, passes the complete native ADBC
suite on an owned Redis Software 8.8 RAM deployment with strict TLS, effective
4x QPF and noeviction. The native library identifies source `b1bc95b`; its
SHA-256 is `10dc9c3519c20a3dd2771b0c08bc3407c9a1a86f79b36fe1c9134fe50848d0a9`.

With Python 3.15.2, PyArrow 26.0.1, ADBC manager 1.13.0, pytest 9.2.0 and
upstream validation commit `47a5ddf3f2a617ac88da3891848e000c74d55b0e`, the result
is **565 passed, 3 subtests passed, 8 capability skips**, in **85.94 seconds**.
Pytest initially collects 676 items; the upstream collection hook removes 103
interactive REPL/query-display helpers, leaving 573 executable cases. This is
the normal complete selection, with no user-provided selection flags.

Test-only source `dd84bc9` updates canonical-prefix assertions, wrong-schema
negative cases, compaction probes and concurrent/crash rekey barriers to retain
the table access identity across generations. All ordinary Go unit packages
pass (root 27.758 seconds). A focused live ordinary selection clears the prior
canonical-storage, rekey/crash, temporary-table, truncation and streaming
failures. That run used a nondefault aggregate cap of 1,000,000: remaining
failures concern exact plan/sort-counter assertions, rather than row results.
The actual ordinary fixture aggregate cap is 2,147,483,648; the Search result
cap of 1,000,000 is a different setting.

The full actual Software snapshot race selection at `dd84bc9`, also with the
nondefault aggregate cap of 1,000,000, completes in 379.052 seconds with 494
passing test events, 29 explicit profile/capability/model skips and one failure.
The failure is the readiness hook counting the new empty `FILTER 0` capability
probe as a data aggregate. No data races are reported. The selection includes
100,001-row bounded compaction and imports beyond capture/candidate quotas.
It does not replace a passing final ordinary-cap snapshot race selection.

The unchanged required `TestSQLRBACSelect` passes under `-race` at `dd84bc9`
in 25.989 seconds, with actual Software API-created restricted identities and
no skipped subtests. This includes metadata discovery/GetTableSchema before
GRANT, row denial, captured-payload privacy, retention, quota, malformed state,
uncertain replies, stable grants, shared roles and DROP/recreate denial. At 128
pins its resource sample remains 20,280/130 bytes (HASH/directory), with the
5,831-byte control script.

An earlier quota run after native validation correctly reached the global
128-pin limit after creating only 125 new readers: preceding completed validation clients
had left three retained/expiring pins. The idle fixture owner explicitly cleaned
only those observed pins before rerunning the unchanged test. CI runs the RBAC
gate before native validation. No quota assertion or admission limit was relaxed.
The final ordinary-cap broad/race and hosted platform gates remain pending.


The independent native TLS check on the same `b1bc95b` library now passes all
**19 partition/lifecycle checks and 6 metadata/statistics identities**. This
includes separate restricted credentials, certificate negatives, full-value and
layout replay, FT.INFO denial with direct-HASH independence, read/write denials,
retained results and producer release. The fixture owner restores the actual
ordinary aggregate cap and re-verifies QPF/noeviction in `finally`.

Test-only follow-up `8f39639` removes the quota fixture's empty-global-state
assumption without changing production admission. Two real administrator-owned
partition producers stay alive while the restricted role fills remaining slots.
An independent administrative read counts exactly 128 live global pins at
refusal; the refused token is absent. Releasing one reader gives 127 and admitting
one replacement returns to 128. Existing expiry/wrong-root checks remain, and
both foreign producers still admit valid workers after the role's pins are
released. Cleanup checks only the test role's inactive namespace.

The complete required SQL authorization fixture passes with this stronger test
under `-race` in **25.486 seconds**. The mixed sample is 126 role pins, 128 global
pins, 19,982 HASH bytes and 130 directory bytes, with the same 5,831-byte script.
The prior 128-role-pin sample above remains a distinct, explicitly isolated run.

## Public-branch integration and independent verification

The public integration maps the staged reader commits to `b6e9ee9`, `944592d`,
`d09d2b2`, `671d4ea`, `8d7e893` and `892ef4a`. Test-only stable-prefix updates
are `49a89d8`; mixed-producer quota coverage is `bf91570`. These preserve the
hidden-cap/direct-partition implementation already published at `09b4e78`.
Production Go and native fixture files match the coordinated `b1bc95b`/`dd84bc9`
source above; the local conflict resolution retains both reader state and the
aggregate-cap discovery flag.

All no-endpoint unit/race packages at `892ef4a` pass: **1,169 test events**,
**1,017 explicit integration/profile skips**, root **53.236 seconds**. This is
compile/unit evidence, not live server compatibility.

An independently owned Software fixture runs the versioned `49a89d8` library
with SHA-256
`4221eb8f3567e839a44ce6d807403af0dd9e5cd526cb34044a169c2b60f19927`:
**19 native TLS partition/lifecycle checks and 6 metadata/statistics identities
pass** with actual separate Software identities. Strict certificates, noeviction
and active/effective 4x QPF are verified; the original aggregate cap and owned
role mappings are restored. Two initial local attempts stopped on incomplete
negative-certificate fixture inputs, before those checks ran; the complete
fixture rerun passes without changing assertions. Certificate keys, identities,
deployment details and full operational reports remain private.

The [controlled-plan correction](controlled-plan-validation.md) preserves
explicit test caps and the distinction between physical cursor primitives and
logical directories. Its 29-event focused race run passes at the actual default
aggregate cap 2147483648. At that checkpoint the complete combined ordinary Go
run and final snapshot race run were still in progress; hosted Darwin ACL fixtures, current
platform/Cluster/recovery, public operation resume and performance parity remain
required. This remains a draft implementation, not a full-parity completion.

The final default-cap complete snapshot race selection at coordinated test
source `7738a7d` now passes **495 test events**, with **29 explicit skips**, root
**575.602 seconds**, all selected packages passing and no data races. This is
the same production source and controlled-plan test correction as public
`a6ea443`; it supersedes the failing nondefault-cap race result only for this
explicitly identified source/configuration. The ordinary run at `bdddd1b`
stops on the additional physical/logical fallback fixture described in the
controlled-plan report; its correction passes 30 focused race events before a
complete ordinary restart. Current hosted ACL/profile/platform, actual Cluster
recovery, public resume and performance requirements remain open.

The complete ordinary restart at `431dcb2` finishes successfully on owned
Software 8.8 with strict TLS, noeviction and verified active/effective 4x QPF:
**2,105 passing test events**, **372 explicit skips**, root **987.730 seconds**.
All six executable packages pass; three packages contain no tests. The actual
ordinary aggregate cap is 2147483648. This closes that source's ordinary-suite
gate; it does not convert skipped backend ACL/profile cases into passes.

The reviewed generic ACL/profile fixes are integrated as `c9870ef` and
`15b8b0a`; [their exact Software profile checks](acl-profile-validation.md)
preserve restricted negative assertions and raw EXECABORT behavior. The narrow
older native cap-response fix is `d1ab283`; its
[red/green and Software evidence](sort-cap-error-compatibility.md) remains
source-specific. Replacement hosted jobs must validate these follow-ups.
