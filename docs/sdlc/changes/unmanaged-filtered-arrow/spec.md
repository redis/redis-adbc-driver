# Contract

Eligible explicit unmanaged bare-column projections may bind simple residual
predicates to reusable decoded scalar positions and append only matching rows
directly to Arrow. Preserve external column decoders, metadata-order decode
errors, all required inputs (even for rejected rows), NULL/three-valued logic,
left-to-right short circuit and ordinary comparison/error behavior. Bind exact
integer/boolean columns, typed literals/parameters, scalar IS NULL, NOT,
AND/OR and comparisons, with at most 32 expression nodes. Large literal IN
lists retain their existing optimized generic path. String/float/decimal/date
predicates, arithmetic/casts/functions, outer/subquery/whole-row references,
retained/subquery work budgets, GPU, ties, late projection and Flex fall back.

Keep LOAD*/HGETALL retrieval, source existence/type checks, Search membership,
readiness, page geometry, cursor cleanup and live mutation behavior unchanged.
Never add hidden fields or modify application sources/indexes. Bare output
aliases/duplicates and exact binary __key values keep their existing rules.

Decode every needed source field before evaluating a predicate, including later
malformed fields on nonmatching rows and the final discovery-page tail after a
consumer LIMIT. Count matched rows/diagnostics and apply OFFSET/LIMIT only after
the residual accepts. Retained Arrow records must survive subsequent reads,
full/early Release and cancellation. No storage/protocol/public-option change.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
