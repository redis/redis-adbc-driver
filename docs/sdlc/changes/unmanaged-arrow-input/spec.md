# Contract and predeclared acceptance

Eligible output belongs to an explicitly unmanaged table, has the existing
minimum width of 64 columns, and uses no optional Arrow byte budget. A separate
per-reader input binding array fuses compatible integer/string coercion and
append. Managed typed bindings, their struct layout and per-column loop remain
unchanged. Other unmanaged types use the original typed coercion/append;
byte-budget, narrow, missing-table metadata and managed paths bind no fused
functions. No row staging, borrowed Arrow buffers or new public option.

Keep complete required metadata decoding, timezone fitting and output coercion
in their original order. Keep skipped/tail metadata errors, exact >2^53 values,
NULL/default/alias/overflow/NaN/temporal behavior, retained record ownership,
terminal partial-row errors, projection predicates, cancellation and cleanup.

Before any measurements, adoption requires all correctness/race/Software checks
to pass without weakened assertions or hidden skips. Native actual clean
baseline/candidate binaries must validate every physical type/value/count and
multiplicity in all 112 trials: managed/unmanaged, one/four readers, 512/4096
public batch ceilings, warmup plus six repeats. Every sample/attempt is retained.
All four unmanaged median elapsed values must improve and their geometric-mean
speedup must be at least 2%. No managed cell may have a >3% median elapsed
regression with nonoverlapping baseline/candidate ranges. No cell may have a
>1% median RSS increase with a candidate range wholly above baseline. Report
all tails and first-batch distributions even when a formal gate passes. Otherwise
keep the candidate unapplied as evidence; do not retune width/mode/type guards,
thresholds or source after timing. Microbenchmarks alone never justify adoption.

Whole-binary ordered blocks on a shared workstation limit causal attribution.
RSS includes Python/Arrow/the driver and is a single worker peak, not Go heap.
See [intent](intent.md) and [plan](plan.md).
