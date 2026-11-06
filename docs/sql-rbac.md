# SQL SELECT grants through Redis Software RBAC

Protocol-16 snapshots support SQL SELECT grants using stable logical table access
identities and reader leases outside the catalog. Existing draft formats are
refused without migration. The driver never grants catalog or row writes to make
restricted readers work.

`GRANT SELECT ON [TABLE] schema.table TO role` and
`REVOKE SELECT ON [TABLE] schema.table FROM role` update a dedicated Redis
Software ACL through its management API. This is enforced key access, not a
compatibility no-op. Provision the Redis role, its users and its database
binding as an administrator before using SQL grants.

The initial implementation supports one permanent, driver-owned base table and
one explicitly named role per statement. Users of the bound role must each
have exactly one Redis role; bindings used by mixed-role users are rejected
before any ACL write. Combined-role authorization is outside this initial
mapping contract. Other privileges, column grants,
`PUBLIC`, role membership grants, grant options, lists, views, adopted/foreign
HASHes, temporary tables, schemas and sequences return errors. Redis Cloud's
management API and OSS `ACL SETUSER` are not implemented by this feature.

## Provisioning

1. Initialize a fresh database with an administrator connection. Existing
   driver storage is not migrated or upgraded.
2. Create a Redis Software role with management access `none`, then assign
   database users to it. The SQL identifier must match the role's name exactly
   after normal SQL identifier folding; quote mixed-case names.
3. Create a **new, dedicated ACL**, named
   `adbc-select-db<DATABASE_UID>-role<ROLE_UID>`, with this initial rule:

   ```sh
   redis-adbc reader-acl -domain adbc-select-db1-role7 > reader-policy.json
   ```

   Replace the example IDs with this database and role. Provision the exact
   `acl` string from that JSON; `namespace` identifies its lease authorization
   domain. The command prints configuration and does not contact Redis. The
   baseline permits positive metadata key families and writes only this domain's
   lease HASH plus directory registration. Captured import values, catalog
   writes and row writes remain denied. Keep each parenthesized ACL selector
   intact when provisioning through the protocol rather than the API.

4. Associate that ACL with exactly this role and database using
   `roles_permissions`. Never reuse it on another database or role, and never
   edit it outside the driver while grant writers are running. Its initial
   rule permits connection/metadata discovery, but no table row access.
5. Configure the **grant administrator's database handle** with the write-only
   string option `adbc.redis.rbac`, containing JSON like:

   ```json
   {
     "api_url": "https://redis-management.example.com:9443",
     "username": "grant-manager@example.com",
     "password": "<management password>",
     "ca_file": "/path/to/management-ca.pem",
     "database_uid": 1,
     "roles": {"analyst": {"role_uid": 7, "acl_uid": 9}}
   }
   ```

   Use the management API origin, with no path, URL credentials or query
   parameters. TLS certificate and hostname verification are required; omit
   `ca_file` to use system roots. Redirects are refused. Configure the Redis
   URI separately, using the **same database** as `database_uid`; administrators
   must verify this association, including any proxy/NAT port mapping. The
   driver does not infer management database identity from a Redis endpoint.

   The management user needs read access to roles, user memberships, all database bindings and
   ACLs, and permission to update ACLs. Redis Software's `user_manager` or
   `admin` management role supplies these API permissions. Query credentials
   need ordinary driver permissions plus `GET`, `SET` (including `NX`) and
   `DEL` on `adbc:{meta}:snapshot:16:rbac:*`, and control access to
   the reader-domain registry. Keep management credentials on trusted
   administrative handles; ordinary readers need only their Redis credentials.
   Reading this driver option never returns its secret configuration.

   Opening this explicitly configured administrator handle validates the role
   bindings and registers their reader authorization domains. Do this before
   restricted Open. Ordinary reader connections discover their single writable
   domain through nonmutating capability checks and never receive API credentials.
   Multiple users may share one SQL role; they share that role's backend ACL
   and lease trust boundary. Per-user raw isolation inside a shared backend ACL
   is not claimed. Different roles retain separate lease write domains.

Use the Redis Software [database-role setup guide](https://redis.io/docs/latest/operate/rs/security/access-control/create-db-roles/)
and [ACL API](https://redis.io/docs/latest/operate/rs/references/rest-api/requests/redis_acls/)
for provisioning. The dedicated ACL contains only additive permissions; resetting command/key
permissions is outside its ownership.
SQL never creates users, changes their memberships, changes
`roles_permissions`, or rewrites other ACL objects.

## Behavior

```sql
GRANT SELECT ON TABLE public.orders TO analyst;
REVOKE SELECT ON TABLE public.orders FROM analyst;
```

A grant adds `%R~<exact-physical-table-prefix>*` to the dedicated ACL. Only a
conservative set of literal prefix characters is supported; unsafe prefixes are
rejected. Read commands and Search commands remain fixed; writes and Search DDL
are excluded. Metadata and index names are visible independently of table grants.
The driver checks the role's name/ID, the ACL's dedicated name, its rules and
exclusive database/role binding before every change. Both statements are
idempotent and report `-1` affected rows.

The configured administrator can query `information_schema.role_table_grants`
and `information_schema.table_privileges`. They expose current table-prefix
SELECT grants for configured roles, with grantor `redis_rbac` and grantability
`NO`. They support dbt's current-grants query. Without management configuration
these views are empty; they are not a complete inventory of Redis permissions.
The companion [dbt adapter](https://github.com/redis/redis-dbt-adapter) enables
this path for explicitly configured native profiles and emits separate statements
for each named grantee. Unsupported privileges fail explicitly. Its live
incremental grant, unchanged-rerun and revoke flow was verified with dbt Core
1.12.6 and an independently authenticated reader.

`REVOKE` removes only this feature's contribution. Independent administrator
permissions can still permit reads. This feature does not override them;
mixed-role users are rejected by the initial mapping. It does not
terminate already fetched results or revoke copies held by clients. API writes
are read back before SQL reports success; enforcement can propagate
asynchronously to proxies/shards. Do not treat SQL completion as a synchronous
barrier for in-flight queries or previously opened cursors.

A table's opaque access identity survives DML, metadata-only rename, TRUNCATE,
type rewrites, compaction, Arrow replacement and managed-copy refresh. Its row
ACL uses this identity ahead of schema/generation components, so replacement
prefixes retain the SELECT grant. REVOKE removes the logical grant across
retained and current generations. DROP/recreate allocates a different identity
and does not inherit the grant. Dropped objects can leave inactive ledger/ACL
entries for administrator reconciliation; old grants may permit raw reads of
old retained keys until revoked or collected.

Reader admission is bounded to 128 live pins and 128 namespaces. Directory/HASH
size checks precede materialization, and lease values are bounded before parsing.
Raw lease/directory writes can still cause denial of service: oversized or
malformed state fails closed and requires administrator repair. Collection never
ignores an invalid directory and guesses which live roots may be deleted.

## Interrupted updates and recovery

Use a non-evicting Redis database and preserve its control metadata. After a
restart, failover or restore that might have lost lock/ledger state, quiesce
grant writers and reconcile API state before resuming. Never expire or manually
modify reserved RBAC keys while writers are active.

All driver grant writers sharing a Redis database serialize through the durable
key `adbc:{meta}:snapshot:16:rbac:lock`. A second writer fails while it exists. There is no
lease expiry: a delayed API request must not overwrite a newer writer after
its lock expires. No atomic transaction spans Redis metadata and the management
API. The ledger at `adbc:{meta}:snapshot:16:rbac:grants:<DATABASE_UID>:<ACL_UID>` is committed
after verifying the API update. An uncertain API write, failed verification or
failed ledger commit leaves the lock in place and returns an error.

To recover, stop all grant writers and ensure any old API request has completed.
Inspect the dedicated ACL and ledger. Restore the ACL to the last committed
ledger (the fixed baseline above, followed by sorted unique
` %R~<prefix>*` tokens from its `prefix` fields), or deliberately reconcile both
to the intended outcome. Read back and verify the result, then delete the lock
and resume writers. Do not remove the lock merely to bypass an unexplained
failure. Preserve the ledger with the Redis metadata when backing up/restoring;
API ACL state and metadata must be reconciled together.

Administrators must also coordinate external binding/ACL changes. Redis Software's
ACL API provides no conditional update used by this implementation; exclusive
ownership of dedicated ACLs is part of the operating contract.

## Verification

`go/privileges_test.go` and `go/rbac_test.go` exercise SQL restrictions, ACL rule
validation, binding isolation, TLS verification and credential redaction.
`TestSQLRBACSelect` provisions its own role/ACL/user in an explicitly disposable,
single-database Redis Software instance. Set `REDIS_URI`, `SSL_CERT_FILE` and
`REDIS_RBAC_TEST_API` (the API origin); optional `REDIS_RBAC_TEST_USER` and
`REDIS_RBAC_TEST_PASSWORD` default to the local Software fixture credentials.
The test alone uses a TLS reverse proxy for the fixture's self-signed API
certificate; production clients always verify the configured API certificate.

```sh
cd go
REDIS_RBAC_TEST_API=https://localhost:29648 go test -count=1 -p 1 -run '^TestSQLRBACSelect$' -v .
```
