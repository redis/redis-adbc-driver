# Design review evidence

Date: 2026-10-22. Author/reviewer: Codex. Human approval pending.
Source: `5cbf91e9968f0d7796ab9c95e0f062fad9522c58`.
Refs [intent](intent.md), [spec](spec.md), [plan](plan.md),
[proposal](../../../active-active-migration-design.md).

Inspected ordinary allocation/write paths (`go/store.go`), ALTER/default
high-water handling (`go/alter.go`), and the exact numeric bounds and scalar
sequence assumption in `go/partitions.go`. These require coordinated changes;
replacing the counter alone cannot preserve all behavior after migration.

Checked official Redis counter semantics and application-failover documentation
linked in the proposal. The proposed ledger is an additional design requirement,
not an existing Redis Cloud feature or tested service.

Reviewed the Redis connections/Search skill guidance and official Redis pooling,
pipelining and regional-index documentation. Added explicit performance gates;
no benchmark or optimality claim follows from this documentation review.
The requestor clarified that performance means ADBC driver performance. The
acceptance contract now explicitly measures public-boundary throughput, latency,
Arrow delivery and driver resource use; server metrics explain those results.
The requestor specifies Software/4x QPF in multi-instance Active-Active topology.
Inspected `go/software-qpf.sh` for effective readback checks; the proposal requires
per-instance verification and Software management discovery. No multi-cluster
Software Active-Active fixture was provisioned or verified for this design.

Correctness/lifecycle review requires issued ID reservation, no wrap/reuse,
default preservation, drained readers and fenced commit/recovery. Ownership
review requires private credentials, controller fencing and no source deletion.
Evidence review distinguishes proposed behavior from tested current behavior.
Ledger backend/fencing, provider transfer, topology identity and profile APIs
remain explicit implementation gates; no human review or runtime pass is claimed.

This branch changes Markdown only. Validate local links and `git diff --check`.
Both checks passed: five proposal/change-record files and 15 local links; the
driver reference also links to the proposal. The owned disposable Software
fixture used for the superseded prototype was removed and its container
inventory verified empty. No new deployment was provisioned for this design.
No server tests are needed to claim documentation validity; no numeric allocator,
migration, WAN-failure, durability or performance qualification is claimed.
