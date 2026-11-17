# Implementation and validation

Date: 2026-11-17. Status: implemented and validated; review pending. Source baseline: `55da2521`.
See [intent](intent.md) and [specification](spec.md).

1. Add bounded build-tagged Go/C probe templates and an archive builder. Reuse
   the archived revision's existing test categorizers without changing commands.
2. Add a native Linux runner with actual Software configuration checks, complete
   typed-result/source-state validation, new-output enforcement and scoped cleanup.
3. Build outside the diagnostic window, then run normal and separate race native
   probes on an exclusively owned verified Software fixture. Preserve failed
   attempts. Record current bridge blockers without bypassing checkptr.
4. Retain sanitized observations/provenance and their limits. Inspect the diff for
   shipping-code changes, sensitive data and incorrect timing attribution. Submit
   a reviewable PR linked to #444; keep its performance acceptance open.

Results and remaining limits are recorded below.

## Results and review

- Final normal Linux/aarch64 Go 1.27.4 diagnostic: all 66 samples and both full
  typed fixtures passed, raw unmanaged HASH/key-set preservation and scoped
  cleanup passed, effective Software 4x QPF verified before/after. No truncated or
  in-flight calls. All ten foreground positions repeated in each measured case.
- Final separate `-d=checkptr=2` library built and reproduced the known
  `main.createHandle` bad-pointer abort before samples. That attempt used the pre-merge source; see the passing post-merge check below.
  Separate race attempt stopped at ThreadSanitizer memory allocation before
  samples. Neither is a safety pass; no bypass was introduced.
- Python syntax compilation passed. Independent checks matched all final
  builder/runner and overlaid-source hashes, 66-sample inventory, call/category
  counts and configuration/state flags. Evidence includes earlier failed builds
  and normal-run hashes; no performance selection or comparison was performed.
- Shipping Go/C files are unchanged. Review checked checked-in artifacts for
  command arguments, endpoints, source values, raw errors and private fixture
  identifiers. The complete result carries only safe categories and hashes.
- [Retained evidence and limits](../../../../benchmarks/crud-execution/results/native-proofs-2026-11-17/README.md).
  This is author review, not maintainer approval. #444 performance acceptance and
  #446 ownership acceptance remain open.

- After PR #462 merged, the same probe on `292ee254` passed all 66 native
  point-read cases under strict checkptr=2, with full typed/raw-state and cleanup
  oracles. Separate source, correctness only; not a latency comparison or full
  #446 safety gate. Both sources' full observations pass the offline auditor.
