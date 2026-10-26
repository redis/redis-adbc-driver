# Specification

GetStatistics with approximate=false performs a bounded, deduplicated HASH
identity scan (one minute, 64 MiB identity budget, two million scanned keys).
It avoids index lag but concurrent writes can change membership: returned values
remain marked approximate. ADBC permits approximate results for exact requests.
This is not completion of exact snapshot statistics.

Retained capture admits temporary tables and views belonging to the producer.
Independent readers replay published output without temporary-source access.
Atomic source snapshots remain unavailable and require transaction/versioning
or coordination with all writers, including application-owned HASH writers.

Execution pools isolate socket interruption from other statements. Commands and
pipelines disable retries; lost write replies can represent committed writes.
Public stream ownership retains the pool through result completion/release.
Cancellation stops client waiting, with no guarantee of remote server termination.

See [intent](intent.md) and [plan](plan.md). This follow-up remains a draft until
full lifecycle, race, ACL and native validation establish the expanded contract.
