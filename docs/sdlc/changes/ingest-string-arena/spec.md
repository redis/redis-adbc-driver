# Contract and predeclared acceptance

Only validated native Arrow String/LargeString arrays targeting exact unbounded
`typeString` and at least two converted rows are eligible. Copy the non-NULL
bytes for the actual conversion rows into one Go-owned immutable string, then
store immutable substrings in Values. The arena is capped at 1 MiB per column;
above it, keep existing per-cell Clone/coerce behavior. No global pool, matrix
reuse, Arrow borrowing or unsafe conversion is introduced. Shared substrings
can retain a whole owned arena when a scalar remains live; this cap bounds that
extra logical payload retention, not allocator/RSS size. Integer, CHAR/bounded VARCHAR/aliases, binary, JSON, temporal,
encoded and unsupported layouts preserve existing coercion and error order.

Full Arrow validation still runs first. Preserve NULL/empty/NUL/Unicode values,
sliced offsets and exact row count, defaults -> columns -> full row checks ->
write ordering, managed whole-operation publication, unmanaged acknowledged
partial effects, no replay and cancellation/unknown-outcome handling. The owned
arena remains valid even after Arrow input bytes are modified or freed.

Freeze before timing: focused unit/race/Software assertions must pass without
weakened assertions/skips. Every mixed-string complete-conversion micro cell must
reduce median allocation counts by at least 50%. Pure integer controls have no
required allocation decrease; the same elapsed/byte regression gate applies.
A micro elapsed or allocated-byte
regression fails when its median is more than 3% higher and the candidate range
is wholly above the control range. Actual clean baseline/candidate native writes
cover both modes, one_stream/one_batch and one/four writers, warmup plus three
measured repetitions per cell (64 total), complete physical types/values/counts/
multiplicity/ordinal validation and raw checks for unmanaged. Keep all samples.
A native elapsed regression fails when its median is more than 3% higher and the
candidate range is wholly above baseline. Native peak-worker-RSS fails at more
than 1% median increase with the candidate range wholly above baseline. Report
all medians, ranges and tails even when those gates pass. Adopt only if every
cell passes these quantitative gates and correctness; otherwise preserve the
exact candidate unapplied. Do not retune guards/cap after results.

See [intent](intent.md) and [plan](plan.md).
