# Intent: bounded Substrait execution

Date: 2026-11-04. Baseline: `f4d5b78`. Requested by Albert Wong; implemented by
Codex. Related: [#213](https://github.com/redis/redis-adbc-driver/issues/213),
[spec](spec.md), [plan](plan.md), [validation](validation.md).

Deliver the runtime phase after PR #261's design milestone: accept a documented
single-table binary plan subset, preserve exact Arrow values and ownership,
reject unsupported semantics before row access, and bound work. The user asked
why the old protocol pin was necessary; assessment found no shipped compatibility
requirement. This implementation therefore pins the current 0.108.0 release.

The latest published upstream Go protobuf module is 0.85.0. Generate bindings
from the exact current upstream schemas while
[substrait-protobuf#7](https://github.com/substrait-io/substrait-protobuf/issues/7)
tracks publishing the missing module. Full Substrait, parameters, partitions,
external code, extension fetching and an independent execution engine are excluded.
