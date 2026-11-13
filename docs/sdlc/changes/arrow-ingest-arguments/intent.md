# Intent: evaluate ingest argument assembly

Date: November 13, 2026. Requestor: repository user; author: Codex.
Status: evaluated and rejected; production defaults unchanged.

Investigate allocation and formatting between checked Arrow conversion slices
and Redis writes after merged source `3213aa54` (production equals `5f759300`).
The first candidate combined exact-size vectors, once-per-slice unmanaged field
boxing and string byte accounting. Its isolated dense gains did not translate
into an established native gain, and a small sparse-write regression was found.
Preserve every result and the unapplied candidate rather than adopting it.

See [contract](spec.md), [plan](plan.md) and [validation](validation.md).
