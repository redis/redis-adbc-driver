# Validation

Baseline: merged main `0359c0d6`; measured harness `434905b9`.
See the [report](../../../../benchmarks/upstream-allocation/2026-11-13/report.md)
and its full sanitized 84-sample evidence. Reproduction commands and ownership
contracts are in the [guide](../../../../benchmarks/upstream-allocation/README.md).

- Isolated go-redis parser race/semantic test passed, including nil versus empty,
  exact integer/binary bytes, complete replies and oversized-buffer alignment.
- Arrow race tests passed, including checked allocator balance, all value/type/null
  checks, retained outputs after source/consumer teardown, early release, descriptor
  reuse and concurrent independent lifetimes. Final checks also passed with
  GOMAXPROCS=4; no dependency on garbage collection for buffer cleanup.
- Minimal two-value Int64 producer-reference test passed. A private overlay that
  omitted the caller's Release failed the explicit reference count and checked
  allocator assertions, confirming the test detects the ownership defect.
- Exclusive GOMAXPROCS=1 benchmarks passed all result checks and all commands
  exited zero. Audit preserved exactly three samples for each of 28 identities.
- Fixtures and pinned sources were checksummed; original dependency licenses are
  preserved by the reproducible private-copy runner. Shared module cache unchanged.

No production behavior was changed. No Redis/Software server or actual query was
run. No server, network, IPC, C-allocation count, peak memory or end-to-end driver
speedup is inferred from Go benchmark counters. Upstream publication is deferred
for parent review. All raw/operational artifacts remain private.
