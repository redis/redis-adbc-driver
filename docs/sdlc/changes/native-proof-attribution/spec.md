# Contract and boundaries

Date: 2026-11-17. Source baseline: `55da2521`. Status: diagnostic only.
See [intent](intent.md), [plan](plan.md), and
[native proof diagnostic](../../../../benchmarks/crud-execution/native_proofs.md).

Archive an explicit Git commit into a new private directory outside Git. Apply
checked, single-match instrumentation insertions to that archive only. Compile
Go/C helpers behind `driverlib,nativeproofprobe`; preserve version linker flags,
record original archive and modified source/library/toolchain hashes. Shipping
sources, SQL semantics, errors, retry policy and forced GC stay unchanged.

Support only Linux clients using a single Redis Software proxy endpoint. Attach
one hook per client and a per-sample context marker at actual Go ExecuteQuery
entry. Report client IDs, safe existing test categories, monotonic durations and
error presence. Do not retain SQL, command arguments, keys, values, credentials,
endpoints or error text. Unmarked calls overlapping a sample remain separate;
context association alone does not prove a call ran synchronously. Background
work beginning before a sample is outside the observer's start-boundary scope.

Cap each sample at 4096 completed calls; expose truncation and in-flight counts.
A ctypes caller owns/frees each returned C string. Begin fails for overlapping
samples; ending a sample freezes its report and never waits for background work.

Retain one warmup and 32 measured diagnostic samples per managed/unmanaged mode.
Completely consume each native result and validate its exact Arrow schema/values,
including NULL, UTF-8/NUL and int64 beyond 2^53. Validate the complete final typed
fixture, unmanaged raw HASH values/key set and scoped cleanup. Verify effective
4x QPF/six workers/mnp32/noeviction before and after, and verified data TLS/RESP3.
An outer supervisor bounds runs. This is an instrumented diagnostic; neither
normal nor race observations qualify a performance target or a GC safety gate.
