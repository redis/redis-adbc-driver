# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

`tupleAggregateShape` admits grouped `SUM`/`AVG` whose input is an exact decimal
expression: decimal or integer columns and non-NULL decimal/integer literals
combined with `+`, `-` and `*` (`exactDecimalSumInput`). Decimal state exists only
in client partial-group mode: if any decimal sum is present and the shape cannot
use partial groups (DISTINCT, COUNT-only grouping, no GROUP BY, or a non-partial
MIN/MAX), the shape is rejected and the existing serial plan runs. Partial groups
force client scans (`server = false`), so decimal inputs are read through the
exact row reader (`scanPages`), never from rounded Search sorting vectors.
Decimal input columns keep `distinctColumn`'s other admission rules (indexed,
simple field name, no `MissingThrough`).

A partition encodes each decimal partial sum as `unscaled:scale`
(`decimalPartial`), computed exactly as `unscaled × row count`. Run merges align
the two scales by multiplying the smaller-scale value by a power of ten and add
(`addDecimalPartials`). The final reduction feeds the merged sum to the serial
accumulator's own `exactSum` as one decimal value and restores the non-NULL count
(`n`), so SUM/AVG rounding, result type and NULL-only behavior are the serial
accumulator's. Integer partials are unchanged.

Expression errors (such as precision overflow) keep the existing contract: a
partitioned InvalidArgument/InvalidData error reruns the serial aggregate.

Acceptance: a partition-level test that fails before the fix (shape rejected)
and afterward matches `serialDriverAggregate` exactly, with types, across forced
row-ID ranges; rejection cases stay rejected; existing partitioned-aggregate
tests pass; SF1 Q1/Q18 complete and validate under the issue's 60 s deadline.
