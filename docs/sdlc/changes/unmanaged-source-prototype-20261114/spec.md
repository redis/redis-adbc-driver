# Exact unmanaged source projection: contract

[Intent](intent.md), [plan](plan.md), [prototype/repro](../../../../benchmarks/unmanaged-source-projection/README.md).

The upstream patch exports a shared rlookup projection core plus a native
RedisKey/RedisString adapter. It has no schema/sorting-vector argument and no
numeric conversion. A single held native HASH handle supplies the selected raw
field values, including absent, empty, binary and exact signed-int64 text.
Present-all-NULL and present-zero-field results remain distinct from missing and
non-HASH source keys. Unselected fields are not enumerated.

Authorization and cancellation/budget gates are mandatory callbacks; authorization
runs before opening the key. Callers must provide actual current-user ACL checks,
not assume OpenKey checks permissions. Gate/native failures propagate as errors,
without partial row publication, retry or conversion to absence. The unsafe native
entry point requires a live context, its owning thread/module lock, and callbacks
that neither yield/unlock nor invalidate the key handle. Returned RedisStrings
must be destroyed before the context expires.

Field count and request bytes are admitted before key opening. Selected result
bytes are checked after native get returns an owned value: this is an accepted
result-size budget, **not a hard peak-allocation ceiling**. Oversized selected
fields may materialize before rejection. A length-only or borrowed native view
would be needed to enforce a pre-materialization bound.

The installed driver and server remain unchanged. Query syntax/planner wiring,
real ACL gate implementation, cursor/coordinator integration, partial-result error
propagation, native module tests and supported Software packaging are explicit
prerequisites. A library prototype is not permission to enable LOAD aliases,
unknown capabilities or approximate integer reducers in the driver. Source reads
would still occur on the server; the intended reduction is client-command fanout,
not elimination of source-data work or proof of an end-to-end speedup.
