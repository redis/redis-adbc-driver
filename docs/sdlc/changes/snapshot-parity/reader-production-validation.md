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
