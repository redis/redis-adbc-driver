# Contract

Date: 2026-11-19. Status: implemented; pending combined PR validation.
Source baseline: `3e45955b`. See [intent](intent.md) and [plan](plan.md).

Named catalogs are persisted in a snapshot-managed metadata hash whose values
are fresh opaque generations. Their schema storage identity injectively encodes
catalog name and logical schema under a NUL-prefixed internal namespace. Public
schema inputs reject NUL; discovery decodes identity at presentation boundaries.
The default `redis` catalog preserves all existing physical schema identities.
Store-level schema enumeration remains global for ownership and stable object
reference resolution; public enumeration filters by catalog.

Catalog creation/deletion requires an active managed write workspace. Registry,
schema membership and object mutations publish atomically through existing root
CAS and are discarded on statement failure. Registry reads participate in normal
metadata dependency validation. A session captures its selected catalog generation;
subsequent statements and implicit metadata lookups reject a replaced selection.
Explicit Arrow catalog destinations do not depend on the connection selection.

Managed same-name tables in distinct catalogs remain isolated. Logical metadata
includes the correct catalog; internal schema identifiers never appear as schema
names. User-visible scope and limitations are in [catalogs](../../../catalogs.md).
No storage protocol version change is introduced; older drivers do not discover
named catalogs correctly and must not access a database using this new feature.

Risks reviewed: reserved-name injection, catalog drop/recreate ABA, partial DDL,
Arrow destination fallback, virtual metadata leakage, cross-catalog rename,
shared physical ownership checks. Catalogs are namespaces, not ACL boundaries.
