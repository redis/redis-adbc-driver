# Specification

Extend the existing TLS and installed-wheel checker with exact global SQL
results, reader-only minimal ACLs, replay after producer closure and source DROP,
tenant denial, local cancellation/early release, and explicit revocation. Workers
receive only their own credentials on stdin and never inherit administrator
connection variables. Keep source ownership and fixture cleanup scoped.

On the declared owned three-primary fixture, migrate only a slot containing
this run's retained namespace. Two persistent native worker connections must
follow ASK and MOVED; denied ASKING must prevent replay until restored. Source
capture during active migration must reject publication. Finalize slot ownership
and remove only owned namespaces, source tables and ACL principals.

Reuse the existing release wheel/client matrix and Software/TLS jobs. Validate
Redis Cloud Pro separately with a new owned non-production database, recorded
quoted costs, fixed short validation purpose and deletion afterward. RESP3 and
certificate verification remain enabled. Record exact revisions and library
hashes, full-fetch oracles, feature skips, and failures without hiding them.

See [intent](intent.md), [plan](plan.md), [evidence](evidence.md).

Foreign-index schema discovery must retain binary document IDs (including NUL
and non-UTF-8 bytes) across RESP2 and RESP3. Use a bounded 128-key Search document-ID
sample and authoritative HASH fetches. Reject malformed IDs rather than dropping
rows or accepting NULL aggregate attribute maps.
