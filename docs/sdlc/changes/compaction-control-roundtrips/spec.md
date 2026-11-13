# Contract

A resumed maintenance step may acquire its durable compaction lease and a fresh
current-catalog read pin at one metadata-slot linearization point. No job or pin
is reused from a preceding step. Keep the existing committed-manifest,
directory/map-cardinality, descriptor, source-prefix/OID/lineage, schema, index
readiness and row-copy ownership/checkpoint proofs. Release the catalog pin
before bounded row copy, as before. Installation retains its own current-root
publication validation.

The combined operation preserves a foreign unexpired lease refusal and the
128-reader limit. Reader-budget refusal happens before lease mutation. No job,
pin or storage-protocol format changes; the original compact-acquire operation
remains compatible with older workers and direct maintenance helpers.

An unknown combined reply is never replayed. The locally known fresh pin token
permits one idempotent bounded release attempt; unavailable cleanup leaves the
existing two-minute lease expiry. Existing worker error recovery reads the
durable compaction record and releases only its own lease. No SQL/Arrow or row
mutation is resent. Keep cancellation, ACL and Cluster declared-key behavior.

Accept only after focused failure/correctness checks and matched native trials
show a supported benefit without a material regression. Preserve all negative
and failed attempts. See [intent](intent.md) and [plan](plan.md).
