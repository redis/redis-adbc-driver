# Intent: native Arrow IPC (#227)

Date: 2026-10-22. Requestor: Albert Wong. Author: Codex.
Source commit: `9493c647aee33916cfc32840b9ba517388e51b50`. Status: implementation.

Provide supported incremental IPC import/export in the native driver, accessible
through the existing ADBC C interface. Albert selected native serialization so
clients do not need to implement it. Split CDC into
[#196](https://github.com/redis/redis-adbc-driver/issues/196) and exclude it
from [#78](https://github.com/redis/redis-adbc-driver/issues/78).

Deliver stream/file interoperability, lifecycle/error coverage, executable
examples and a clear matrix separating incremental input from output. No SQL
operator eligibility expansion, continuous feeds, new Redis wire format, spill
engine or universal memory guarantee is included. Further incremental SQL
execution remains open in #227.

See [spec](spec.md), [plan](plan.md), and [evidence](evidence.md).
