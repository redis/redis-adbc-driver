# Contract and predeclared acceptance

Only validated native Arrow String/LargeString arrays targeting exact unbounded
`typeString` and at least two converted rows are eligible. Copy the non-NULL
bytes for the actual conversion rows into one Go-owned immutable string, then
store immutable substrings in Values. The arena is capped at 1 MiB per column;
above it, keep existing per-cell Clone/coerce behavior. No global pool, matrix
reuse, Arrow borrowing or unsafe conversion is introduced. Shared substrings
can retain a whole owned arena when a scalar remains live; this cap bounds that
extra retention. Integer, CHAR/bounded VARCHAR/aliases, binary, JSON, temporal,
encoded and unsupported layouts preserve existing coercion and error order.

Full Arrow validation still runs first. Preserve NULL/empty/NUL/Unicode values,
sliced offsets and exact row count, defaults -> columns -> full row checks ->
write ordering, managed whole-operation publication, unmanaged acknowledged
partial effects, no replay and cancellation/unknown-outcome handling. The owned
arena remains valid even after Arrow input bytes are modified or freed.

Freeze before timing: focused unit/race/Software assertions must pass without
weakened assertions/skips. Complete conversion microbenchmarks must materially
reduce mixed-string allocation counts without a clear elapsed/allocated-byte
regression. Actual clean baseline/candidate native writes cover both modes,
one_stream/one_batch and one/four writers, warmup plus three measured repetitions
per cell (64 total), complete physical types/values/counts/multiplicity/ordinal
validation and raw checks for unmanaged. Keep all samples. Adopt only if no clear
native elapsed or peak-worker-RSS regression appears; otherwise preserve the
exact candidate unapplied. Do not retune guards/cap after results.

See [intent](intent.md) and [plan](plan.md).
