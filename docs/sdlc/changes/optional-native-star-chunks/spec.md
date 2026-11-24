# Specification

See [intent](intent.md), [plan](plan.md).

Round-robin workers scan globally contiguous exact int64 chunks, targeting256
allocated row IDs with at most65,536 chunks and the existing MaxInt64 final tail.
Quotient/remainder-spread bounds keep each finite chunk floor/ceil sized;
lo+(n-1) avoids MaxInt64 intermediate overflow. Legacy coarse range helpers
remain unchanged. Per-worker one-event queues carry bounded output pages and explicit completion,
source-error and panic markers. The coordinator preserves global chunk order,
first-source-error priority and group discovery; reducer failure stops probing
but retains subsequent source-error detection. Cancellation/panic joins all
workers before grants/pins can release; explicit didPanic retains panic(nil).

Only the pre-existing eligible direct published native CPU path chooses owned
facts and page chunks. Shared helper compatibility remains shared-safe/coarse;
GPU path remains coarse/shared-safe. Each worker owns one executor/cache reused
across its chunks; no evaluator state is shared across workers. Full original
fact residual and prepared dimension demand stay unchanged.

Existing rowIDRangeScan.cap is a per-subrange scan/batching cap, never an SQL
result LIMIT (partition_caps.go continues opening through the original hi).
The forced SnapshotOverlay path never enters numeric Search rowIDRanges. Its
exact-directory branch may use positive cap only to clamp directory/output
page sizes to at most256. lo/hi/through, winner/tombstone merge, source counts,
row identity, missing-row errors, pin guards and total results are unchanged.
