# Plan

1. Restore the verified compatible full-scale backup on task-owned Redis Software
   with verified active 4x QPF, TLS and RESP3; preserve checksum/source identity.
2. Build matched off/on binaries from merged main with the same Go toolchain and
   flags. Prove actual path selection outside timing.
3. Repeat the complete 100K × 105 and three-column regular/partition, one/four
   reader matrix with one warm-up and twenty alternating-order measured pairs.
   Validate exact values/types/NULLs/multiplicity outside timing. Retain failures.
4. Record client CPU/allocations, sampled live Go heap, independently sampled
   per-process RSS and request/reply bytes. Do not interpret inherited Linux
   child ru_maxrss as a precise per-query memory measurement.
5. Run lifecycle/race and fallback tests plus representative shared-library
   checks. Assess concurrent reader nonregression and per-reader extra-page cost.
6. Promote only if final-runtime wide gains repeat, concurrent cases show no
   material repeatable elapsed regression, memory is accounted for and lifecycle
   tests pass. Otherwise retain opt-in and document the concrete remaining cause.
7. Clean up task resources, publish sanitized evidence, and create a follow-up PR.
