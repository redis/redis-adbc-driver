# Specification

Date: 2026-11-20. Source base: `27e5a8d2`. Requestor: repository user. Author: Codex.
Issue: [#554](https://github.com/redis/redis-adbc-driver/issues/554). Status: implemented; focused validation passed.

Unordered single-table row-local SELECTs use demand-driven SCAN discovery, native HASH images and the existing Arrow stream reader. Predicates recheck authoritative values; LIMIT/OFFSET and row/byte budgets use existing stream semantics. Streaming-required admission rejects blocking shapes before source scans. Managed/unmanaged execution keeps its existing contract.

SCAN and source HASH pipelines never replay unknown outcomes. Duplicate keys are suppressed across pages; nested collections are excluded and binary suffixes preserved. Missing/non-HASH source rows are skipped. Definition identity and index readiness are checked around fetches and before every emitted batch, including buffered batches and initially exhausted results.

Discovery retains one server SCAN reply, whose COUNT is advisory, and O(observed source keys) deduplication. HASH fetch requests cap at 128 keys/1 MiB target with a 64 MiB single-key ceiling; native reply sizes remain application controlled. At most one fetched page can overshoot output batching; an oversized single row is allowed. Cancellation/Release clear discovery state and Arrow buffers through existing ownership cleanup.

Source edits can interleave; buffered values may reflect earlier reads. A sparse predicate may scan all keys before producing a batch. No stable result replay, sort order, cross-process partitioning or atomic source capture is promised.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
