# CRUD execution diagnostics

These observers explain where work occurs; the complete uninstrumented native
comparison supplies performance claims. Run diagnostics separately from native
timing windows, builds and other tests on an exclusively owned Redis Software
fixture created with `make -C go redis-up`. Verify active/effective 4× QPF, six
Search workers, mnp/32, noeviction and trusted TLS/RESP3 before execution.
Credentials, owner identifiers and operational logs remain private.

## Go protocol observer

[`TestCRUDExecutionProfile`](../../../go/crud_execution_profile_test.go) requires
`REDIS_CRUD_EXECUTION_PROFILE=1` and a verified disposable `REDIS_SNAPSHOT_URI`.
It retains managed, unmanaged and raw client reads/INSERT/UPDATE/DELETE: 12 cases,
each with one warmup and 32 measured distinct operations over a 1K-row/four-public-
column fixture plus an ordinal. The raw read uses HMGET and typed Arrow
construction. Reads fully fetch typed values; complete resulting fixtures and
physical schemas are independently checked after timers.

The observer uses the actual execution pool, shared store and background worker
clients, with one hook per client role. Per-operation context tags distinguish
foreground calls from outside-operation validation, worker and cleanup calls.
The latter remain in separate timelines; their overlap is not added to operation
latency. Shared-client calls without the operation tag are not assumed to be
caused by the currently running statement.

Each sample retains execution-scope acquire/finish, SetSqlQuery, ExecuteQuery or
ExecuteUpdate, and fetch/release wall times, plus ordered command/pipeline shapes,
command counts and hook elapsed time. A hook invocation is **not a measured
packet round trip or server CPU duration**. It can include connection/pool wait,
transport, server work and decoding. Pipeline command counts remain distinct
from hook invocations. The frozen baseline value-byte observer covers returned
String/Slice/map values only. A later observer-only correction unwraps the known
unmanagedProjectionCmd/onceCmd no-retry wrappers and counts flat generic string/
byte values; generic nested replies remain deliberately partial. Earlier counts
retain their original observer hash and are not retroactively corrected. These
counters exclude RESP framing, keys and Lua-internal values; zero for unsupported
reply types does not establish zero transfer. Neither command counts nor
client durations are a causal CPU decomposition.

The explicit outer execution scope permits hooks to be attached before its first
command; normal SQL acquires fresh catalog/row proofs inside that scope. This is
instrumented Go attribution, not the C/Python ABI workload. Mutations here use
ExecuteUpdate, whereas ordinary Python DBAPI `execute()` uses ExecuteQuery.
Failed samples remain unvalidated, and cleanup logging retains their partial
observations. A hook error can be an expected native NOSCRIPT/refusal during
warmup; final test/sample/fixture validation determines workload success.

Example after building the test binary in the recorded Linux environment:

```sh
REDIS_CRUD_EXECUTION_PROFILE=1 /private/crud-profile.test \
  -test.v -test.count=1 -test.run '^TestCRUDExecutionProfile$' -test.timeout=12m
```

## Ordinary native cursor observer

[`native_stages.py`](../native_stages.py) wraps the unchanged normal
[`scenarios.operation_call`](../../table-modes/scenarios.py). Its transparent
cursor proxy delegates to the same real Python/ADBC cursor and retains execute,
complete fetch_arrow_table, rowcount and total operation wall times. Raw Redis
commands and conversion are unchanged. Original typed-result, count/multiplicity
and complete mutation post-state checks remain in `results.json`.

```sh
python3 benchmarks/crud-execution/native_stages.py \
  --driver /private/exact-build/go/build/libadbc_driver_redis.so \
  --cli /private/exact-build/go/build/redis-adbc \
  --version EXACT_RECORDED_VERSION --work /private/new-native-stage-run \
  --dedicated-empty-server --suite oltp --repeats 1 --operations 32
```

This produces 48 original trial records, including warmups, and 1,536 operation
records in `native-stages.json`. These are instrumented diagnostic observations;
proxy/timer overhead is included. The ordinary three-round matrix is separate.
The exact wrapper hash, driver source/version/binary, packages and original
scenario hashes must identify each run; never relabel an earlier wrapper run.

Set `REDIS_CRUD_NATIVE_SUBSTAGES=1` for additional nested observations on the
pinned DBAPI's actual `_RowIterator.close` and `_blocking_call` operations:

- `previous_result_close`: closing the preceding result inside the current
  cursor.execute, exactly where the original benchmark charges it.
- `adbc_prepare` and `adbc_execute_query`: the actual blocking-call targets,
  preserving the original cancellation callback, arguments, results and errors.
- `arrow_read_all`: the existing full result read, without replacing the reader
  or changing its ownership/lifetime.

The wrapper calls each original method once and adds no closes, retries, fetches
or schema calls. Thread-local operation tags are restored in `finally`, including
on errors. These nested spans overlap their outer cursor/operation spans and
**must not be summed with them**. Subtracting independently pooled medians is
also invalid as an additive decomposition.

With 32 operations and fresh per-thread clients, each one-client trial observes
31 previous-result closes, and each four-client trial observes 28. Each thread's
first call has no preceding result; its final cursor close is outside operation
timing. Consequently a previous-close percentile has 31/28 observations per
trial, not 32. Lazy first-operation transport work remains included. A measured
close span is not itself an exclusive GC/CPU measurement.

## Verified pinned Python call sequence

Installed source from the actual Linux benchmark image, package
`adbc-driver-manager==1.13.0`, was inspected with networking disabled. The owner
records hashes of `dbapi.py`, `_lib.pyx`, `_reader.pyx` and `_blocking_impl.cc` in
run provenance. For unbound literal SQL:

1. `Cursor.execute` calls `_clear`, which closes the previous `_RowIterator`.
   An imported reader calls `reader.close()`; an unimported stream calls its
   handle's `release()`.
2. On changed SQL text, `_prepare_execute` calls `set_sql_query` and `prepare`.
   It does not implicitly call GetParameterSchema or ExecuteSchema.
3. The driver manager calls `AdbcStatementExecuteQuery`, including for DML,
   then constructs a lazy `_RowIterator`. It does not eagerly import the Arrow
   stream or request its schema in that constructor.
4. `fetch_arrow_table` imports the reader when first needed and calls `read_all`.
   The reader remains attached to the cursor until the subsequent clear/close.
5. The pinned `_blocking_call` directly invokes its target on non-main Python
   threads, as used by the scenario workers; its main-thread SIGINT setup is not
   evidence for the worker-thread latency.

Current [`RedisArrayStreamRelease`](../../../go/pkg/driver.go) explicitly calls
`runtime.GC()` after releasing its reader. The native close observer measures the
whole corresponding close path, not GC alone. Current nested observations put
one-client previous-close medians around 0.40–0.50 ms, while actual
`adbc_execute_query` still accounts for a larger measured span. Closing is a
measured opportunity, not an explanation for the entire gap between the separate
Go and Python experiments.

## Follow-up boundaries

No ordinary GC policy or Arrow lifetime behavior changes are selected by these
diagnostics. The [earlier forced-GC experiment](../../ffi-release-gc/2026-11-13/report.md)
reduced isolated stub release costs but did not improve its complete native bulk
reads and retained blocked sanitizer/ownership gates. Those negative findings
remain separate from current point-operation attribution.

Useful next experiments are narrowly bounded: prove explicit C/Arrow release
and retained-buffer lifetimes for a specific stream path, then measure its
cleanup policy under normal native CRUD; or investigate an ExecuteUpdate-aware
caller API that avoids allocating an unused DML result stream. Do not change
DBAPI execute semantics, discard RETURNING results, or infer foreign-resource
ownership solely from an empty schema. ASAN-only GC guarding would still change
normal imported-buffer callback timing and needs its own ownership/sanitizer,
cancellation, memory and full native regression gates.

Preparation is not a demonstrated performance fix here. Existing SetSqlQuery
reuses only unchanged SQL syntax; Prepare parses it, while catalog/type/authority
binding stays fresh. A separate bound-parameter SELECT can incur an additional
planning snapshot. No prepared-vs-literal live comparison or new plan cache is
part of the observations above.
