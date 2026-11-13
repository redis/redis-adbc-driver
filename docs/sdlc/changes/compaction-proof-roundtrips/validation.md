# Validation

Date: November 13, 2026. Source review only; no builds/tests/profiles run yet.
Base: `24f654b76b98f3d8960af94a3ca0230185d467a1`.

The manifest/directory/map counts already share a native pipeline. The root-map
read follows its committed-manifest/cardinality proof; fetching it earlier could
read an oversized corrupt map before the existing 256-bucket bound. Bucket entry
reads already share their directory/cardinality/reference proof pipeline. The
physical descriptor key is known only after that entry resolves, and its GET
follows an object reference/existence gate. Pipelining GET with that gate issues
it even when ownership, reference or ACL proof fails. Both proposals are rejected.

Segment FT.INFO and directory-cardinality checks already share a bounded pipeline.
Copy source-directory ranks/pages and physical row images are already batched
after their preceding proofs. Copy commit and lost-reply resolution are dependent
on the durable checkpoint and cannot be sent together. Index-phase FT.INFO and
output ZCARD offer at most one independent call per job; that is insufficient
reason for a production change to a track investigating per-copy-step overhead.
A new Lua proof-plus-GET operation would change parsing/ACL/error paths for a
small hypothesized benefit; it is not proposed without stronger evidence.

Only the opt-in diagnostic gains supplemental pipeline-shape counts. All
production files and default behavior remain unchanged. Execution results,
source freeze, environment verification and final cleanup are pending the
parent's explicitly serialized slot. No ClickBench is planned.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
