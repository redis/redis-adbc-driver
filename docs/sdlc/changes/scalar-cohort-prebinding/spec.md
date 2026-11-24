# Specification

See [intent](intent.md), [plan](plan.md), [validation](validation.md).

After the cohort's columns are finalized, construct lane column indices in metadata order, predicate-field indices in the original compiled predicate order, and the AVG input index. COUNT keeps an unused input index of -1 and exits through its existing Star branch. Missing needed/predicate/input fields, duplicate logical column bindings or unsupported function shapes refuse the cohort before execution. Publish no partial lane bindings: validate all temporary configurations before assigning any lane.

The row loop uses only proven indices. It retains shared decoded value/error slots, metadata-order error selection, decode-before-WHERE even on rejected rows, default/NULL marker semantics, exact accumulator updates and lazy finalization. Source discovery, physical fetches, registration limits, snapshot/namespace checks and CASE/outer-row gates are unchanged. This optimization is separate from the required direct-Nil HASH-read hardening in [#609](https://github.com/redis/redis-adbc-driver/issues/609); integration must retain that fix too.
