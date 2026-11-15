# Exact unmanaged source projection: validation

Date: 2026-11-14. Driver baseline `d7c0978f29ee57ee7d18fc4f67e2dba2b992d0ab`.
Upstream Search source `33d90342bc9c601d904fb58446f8bda4eaba5011`, native
dependency redismodule-rs `a808377ffde496d2cb8bcf26ee76a52080da9d89`.
[Intent](intent.md), [contract](spec.md), [plan](plan.md),
[report](../../../../benchmarks/unmanaged-source-projection/README.md).

## Rust source verification

The host initially lacked Rust. The coordinator authorized an isolated official
Rust 1.100.0 toolchain installation under the task's private artifact directory.
Every component archive SHA256 matched the HTTPS official manifest; no global
PATH/profile changes were requested. Process groups were checked/closed before
returning the exclusive build slot. Exact components and command outcomes are
in [checks](../../../../benchmarks/unmanaged-source-projection/checks.json).

- The exact proposed source core compiled and ran: seven tests passed, zero
  failed/ignored. Covers signed-int64 extrema/adjacent values, actual NUL and
  non-UTF8 field/value bytes, NULL/empty/presence/type, authorization before open,
  cancellation at every gate, unknown native error without partial row/retry,
  request/result admission and only selected-field reads.
- The native RedisKey/RedisString adapter passed `cargo check --lib` against
  its real pinned dependency and checked-in Cargo lock. This was a small isolated
  native-API type-check harness, not the full Search workspace/module build.
- The zero-context patch applied to the exact pinned base with
  `git apply --unidiff-zero`; applied source bytes match the compiled artifacts.
- Source review corrected the native public function to unsafe with explicit
  context/thread/lock/no-yield/lifetime requirements. The byte budget is explicitly
  post-materialization result admission, not a peak-allocation ceiling.

## Actual Software RESP3 / ADBC repro

The coordinator ran frozen harness source `186439ca` with the frozen baseline
Linux native shared library and CLI. The harness hash in
[live evidence](../../../../benchmarks/unmanaged-source-projection/live-evidence.json)
matches the current source. It used an exclusively owned initially empty Software
fixture, verified active/effective 4× QPF/six Search workers/mnp32/noeviction,
strict certificate/hostname TLS and RESP3. The Linux client joined the same
Software TLS network namespace, avoiding host macOS published-port forwarding.
Client/server resources, package versions, binary hashes and cleanup metadata are
in [live provenance](../../../../benchmarks/unmanaged-source-projection/live-provenance.json).

- Exit zero. Seven complete typed SQL queries validated 23 projection rows plus
  three aggregate rows, including int64 extrema, adjacent values above 2^53,
  NULL, empty strings, actual NUL, live changes, deletion and wrong-type omission.
- Independent raw HASH controls verified exact native values and binary fields.
  The current driver remained correct. No prototype server capability was used.
- Ordinary aliased LOAD produced numeric/raw-byte mismatches for cases 2/4/5:
  int64 maximum, 2^53+1 and -(2^53)-1. All eight cursor rows were retained/audited.
- Full LOAD* preserved selected bytes, but returned 131,230 value bytes excluding
  document keys versus 126 selected public-value bytes. This synthetic overfetch
  case is not measured wire bytes, server CPU or a SQL performance comparison.
- Existing all-selected-NULL and missing-source HMGET vectors were identical;
  separate native TYPE controls differed. They are controls, not one atomic proof.
- Source HASH/string/binary values and the external application index survived
  SQL DROP. Owner-checked disposable cleanup passed; client container/private
  environment removed. All subprocesses/clients ended before root's next timing.

## Scope and review

The prototype is **not installed** in Redis Software and no driver runtime file
changes. The callback tests do not implement/prove actual native RBAC, query
cancellation, cursor/coordinator error propagation, TTL behavior or engine memory
integration. Parser/planner wiring, real ACL gates, cursor/distributed ownership,
warning/fatal-error handling, a compatible Software artifact and native SQL
performance comparison remain prerequisites. Source reads would remain internal
server work even if client fanout were removed.

Root source review identified the unsafe-context and post-materialization-budget
clarifications before accepting the prototype. Actual core/native API checks and
baseline repro were audited; no human maintainer approval or merge is inferred.
No upstream issue/comment, release, new driver mode or ClickBench run was created.

### Final callback-contract review

Root review requested an explicit prohibition on source-key mutation while its
native handle is open, even if the module lock stays held. The unsafe #Safety
contract, shared core docs and integration contract now forbid callbacks from
mutating/deleting/replacing/expiring the source key or its HASH field/value
storage, or executing commands/module APIs that invalidate the held RedisKey or
borrowed storage. This is a caller obligation, not something arbitrary callbacks
can be proven to obey by mock tests.

The exact final Rust files were reformatted, recompiled and rerun: seven core
tests passed, zero failed/ignored; the native adapter passed locked/offline
Cargo check against the same real pinned dependency. No invalid pointer/handle
or UB test was executed. [Final source/binary hashes and outcomes](../../../../benchmarks/unmanaged-source-projection/final-safety-checks.json)
match the regenerated patch; initial source hashes remain separately recorded.
The SQL repro source and its earlier Linux results are unchanged. No Redis
operation or Go/native benchmark was rerun for this comment/contract clarification.
