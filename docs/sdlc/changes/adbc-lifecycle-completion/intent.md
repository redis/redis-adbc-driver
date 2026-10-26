# Intent

2026-10-22. Author: Codex; requestor: Albert Wong. Base: 7a62902 (merged #492).
Continue #476, #477 and #478 through implementation/lifecycle verification and
accurate public contracts. Status: implementation and local verification complete; maintainer review pending.

Finish cancellation coverage for partition APIs, preserve rollback/lease and
Arrow reader ownership, restore diagnostics across isolated pools, and reconcile
statistics/temporary-source documentation with the merged runtime.

The issue scopes explicitly allow statistics scoping and ask for an atomic
source-capture feasibility assessment. This change does not claim an atomic
snapshot or exact snapshot count. Those require the transaction/versioning
work in #269; returning an approximate statistic for an exact request follows
the ADBC schema flag contract. UNIQUE/FOREIGN KEY enforcement remains #270.

See [spec](spec.md), [plan](plan.md), [evidence](evidence.md).
