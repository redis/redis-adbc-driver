# Intent

Date: 2026-11-12. Requestor: repository user. Base: `8ed19932`.
Status: implemented and locally validated; see [validation](validation.md).
Proposed in [PR #404](https://github.com/redis/redis-adbc-driver/pull/404).
Maintainer approval and release are not claimed.

Fix unmanaged ingestion's serial round trips and repeated full-table range scans
observed in the [table-mode benchmark](../../../../benchmarks/table-modes/post-arrow-2026-11-12/report.md).
Preserve source ownership, live reads, explicit-key INSERT conflict refusal and
non-replayable mutations. See [contract](spec.md), [plan](plan.md).
