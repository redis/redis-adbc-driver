# Contract

Date: 2026-10-21. Status: implemented, human review pending.
See [intent](intent.md), [plan](plan.md), and issue #400.

The preparer accepts an explicit Docker container and localhost API/database
ports. Before API access, verify the Compose service label and both exact
127.0.0.1 port bindings. Require a single matching RAM database named adbc-test,
requested shard count, disabled TLS and no existing role mappings. New fixture
files use the existing restricted certificate generator; private material stays
local. Install API/proxy certificates, require data TLS/password authentication,
verify the API CA, actual active primary count and real aggregate cap 137, and
prove plaintext cannot obtain a Redis reply. Initial bootstrap management access
uses the owned localhost self-signed API, as the existing setup does; subsequent
management and all native data requests verify the generated CA.

Software blocks protocol ACL SETUSER. Use database-only roles, ACL rules, users
and per-database role mappings via verified REST APIs. Record each created UID;
cleanup removes only those users/mappings/roles/ACLs, continues after individual
failures and reports any failure. Wait for authenticated PING and explicit ACL
permission propagation before result assertions. Retry only HTTP 409-rejected
idempotent PUT /bdbs/<uid>, bounded to 120 seconds; never retry POST creation,
failed SQL or incorrect query results. Request/response bodies and credentials
are never logged or uploaded.

Reuse all 15 TLS checks: native wrong CA/hostname, closed planner and independent
read-only workers, exact Arrow values/schema, direct HASH predicates, malformed
and stale descriptors, denied FT.INFO/HMGET/administrative writes, cancellation,
early release and live-read semantics. Software returns an empty CONFIG GET for
unauthorized users rather than NOPERM; assert that exact empty reply as a 16th
check and still require CONFIG SET/HSET denial. OSS keeps its CONFIG GET denial.

Require both Software topologies on both Linux PR architectures, and installed
Software TLS wheel checks in both Linux release jobs. Mutate TLS only after the
existing non-TLS suites finish. Upload only result JSON; delete private material.

API references: [certificates](https://redis.io/docs/latest/operate/rs/references/rest-api/requests/cluster/certificates/),
[database roles](https://redis.io/docs/latest/operate/rs/7.8/security/access-control/create-db-roles/),
[TLS](https://redis.io/docs/latest/operate/rs/security/encryption/tls/enable-tls/).
