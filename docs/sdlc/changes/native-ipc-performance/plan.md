# Plan: IPC performance

Date: 2026-10-22. Author: Codex. Base: `e2e7725`. Status: complete; review pending.
See [intent](intent.md) and [spec](spec.md).

1. Start an owned local Redis Software fixture and verify effective QPF 4x.
2. Pin current merged source/native binary and Python/Arrow versions.
3. Implement matched fresh-process runner and smoke-test all 24 cases with
   small fixtures, including complete independent validation and cleanup.
4. Execute one warmup and five measured trials per case; alternate method order.
5. Check data/schema/batch parity and summarize medians/ranges for elapsed,
   throughput, CPU, process RSS and encoded size; report regressions as well as
   gains and explain what the measurements can establish.
6. Review runner/evidence, remove owned fixture/container/network, and publish a
   report with reproducible commands and checksums. No driver behavior changes.
