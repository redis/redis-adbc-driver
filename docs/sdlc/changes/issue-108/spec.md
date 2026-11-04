# Specification: dedicated SELECT ACLs

Date: 2026-11-04. Source: `f1e1f03`. Status: implemented and locally verified.
See [intent](intent.md), [plan](plan.md), and the [public contract](../../../sql-rbac.md).

Accept exactly one table/role SELECT object privilege per statement. Require
explicit write-only management configuration with verified HTTPS, database ID,
role/ACL bindings and separate credentials. Validate dedicated ACL ownership,
exclusive binding, role name/ID, single-role user memberships, and canonical fixed read-only rules. Never
change role membership, database bindings or unrelated ACLs.

Use read key patterns for exact owned-table prefixes, refusing other object
kinds and unsafe prefixes. Expose configured current grants through both
information_schema grant views. Preserve metadata-only rename grants. Prefix
changes deny new data until re-granted; stable table identity lets revoke remove
an old physical-prefix grant. Recreated objects do not inherit access.

Serialize driver writers with a non-expiring Redis lock. Verify management
writes before committing a ledger; retain the lock on uncertain writes or
ledger failures. Recovery is explicit operator reconciliation. External ACL
editors must coordinate; there is no cross-system transaction or API CAS.
API/database endpoint association is an explicit administrator responsibility.

Acceptance: real Software user denied before grant, reads complete correct rows
after grant, cannot read another table or write, and is denied after revoke.
Also verify idempotence, catalog discovery, rename/truncate behavior, ambiguous
write locking, TLS, secret redaction, and rejection of unsupported privileges.
No Cloud control-plane, OSS ACL provisioning, full SQL authorization semantics,
view grants, synchronous revocation barrier, or unrestricted dbt privilege support is claimed.
