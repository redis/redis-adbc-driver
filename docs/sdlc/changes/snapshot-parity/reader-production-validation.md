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
