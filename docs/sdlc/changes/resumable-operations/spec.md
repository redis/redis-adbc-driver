# Recovery and ownership contract

Date: 2026-11-09. Issue: [#335](https://github.com/redis/redis-adbc-driver/issues/335).
Status: implemented; focused local validation passed; delivery checks pending.

Capture must close and validate before an operation can detach. Imports retain
exact typed journal values and binary source keys, with stable source-to-row-ID
bindings. SQL/Arrow preparation must finish evaluation/input consumption exactly
once; incomplete capture is abort-only. Resumption continues private output,
index preparation and publication without executing application input again.

A durable operation needs bounded independent retention and renewable exclusive
owner tokens checked at every mutation. Takeover fences an old process even if
its socket/context survives. Retention protects the original base and private
references independently of a process-local pin. Abort/expiry/commit retire those
references through the existing ownership graph.

Persist semantic metadata reads/dirty/prune/change sets and the target/schema/
index policy before detachment. Rebase unrelated publications; reject changed
targets/dependencies. Preserve temporary-object ownership and schema/rekey guards.
Index continuation requires durable phase/outcome proofs; uncertain native Search
commands must not be resent. Runtime-interrupted mutations remain abort-only.

Publication records its terminal outcome atomically with the catalog change.
Status resolves unknown replies read-only. No background recovery may publish a
failed statement. Native and ADBC handles expose unambiguous status/resume/abort
and documented limits without embedding endpoints or credentials.

Introduce an explicit storage protocol fence before detached ownership can be
enabled. Preserve fresh-database/no-migration behavior, ACL/TLS and current managed
copy/refresh semantics. Prove recovery in separate processes at capture, partial
output, index preparation and lost-publication boundaries, plus stale owners,
competing resumers, cancellation, abort/expiry, source changes and collection.
Actual Software Cluster, promotion and AOF/RDB runs require verified effective
4x QPF. Historical saved-branch results do not validate new code.

See [intent](intent.md) and [plan](plan.md).
