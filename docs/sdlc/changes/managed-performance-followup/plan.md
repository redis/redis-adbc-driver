# Plan

1. Establish owned verified Software fixture; collect base CPU/command profiles.
2. Reproduce excess work with correctness/command-count regression cases.
3. Implement bounded improvements supported by profiling; preserve safety contracts.
4. Run affected race/broader checks and matched baseline/candidate benchmarks.
5. Review, publish sanitized evidence/PR, and remove owned resources.

See [intent](intent.md), [spec](spec.md).

Steps 1–5 completed. See [validation](validation.md) for the reproduced excess
work, changes, test coverage, native/diagnostic observations, memory tradeoff and
cleanup. Fixed-call write throughput remains a limit; larger-call results are
separate usage controls.
