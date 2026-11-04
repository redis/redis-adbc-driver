# Plan: investigate #215 and measure the consumer prototype

Date: 2026-11-03. Investigation source: `95d6583`; native implementation base:
`6d7ac40`. Status: native implementation and validation complete; maintainer review pending.
See [intent](intent.md) and [specification](spec.md).

1. Inspect pinned ADBC/Arrow APIs and CPU query/aggregate paths: complete.
2. Document host staging choice and deferred native C Device contract: complete.
3. Add opt-in CPU/cuDF experiment with independent validation: complete.
4. Run CPU tests, synthetic smoke and real CUDA trials: complete; seven CUDA
   tests passed and nine timed synthetic trials validated.
5. Run Redis Software/4x-QPF comparison: complete; one-million-row fixture fully
   validated, three timed trials passed. Input fetch dominated GPU computation;
   the modeled GPU consumer path did not beat direct SQL.
6. Self-review correctness/lifecycle, ownership and evidence using `REVIEW.md`:
   complete. Actual results are in [validation](validation.md). Maintainer review
   and issue disposition remain external to this investigation.
7. Implement native Linux CUDA integer predicates and C Device staging/stream
   extensions following the user's expanded scope: complete. All 11 native tests pass on NVIDIA A10G, including real CUDA
   predicates, producer events, cancellation and Arrow C Device consumption.
8. Transplant onto current redis/redis-adbc-driver main, retain current dependency
   and release improvements, rerun checks and create the requested PR: complete.
