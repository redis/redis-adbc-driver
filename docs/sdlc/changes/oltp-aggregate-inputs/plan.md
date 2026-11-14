# Plan

[Intent](intent.md), [spec](spec.md). Author: Codex. Human review pending.

1. Inspect planner exclusions and runtime precision proofs; distinguish output
   CAST traversal from input reducer eligibility.
2. Add narrowly scoped immutable BIGINT runtime eligibility in query.go.
3. Share ephemeral aggregate projection for direct scalar/grouped inputs and
   live unmanaged supported predicates without bypassing source checks.
4. Verify exact numeric boundaries, NULLs, output types/casts, snapshot masks,
   mutation/deletion races, callback failure, cancellation and unchanged fallback.
5. Coordinate Software tests on parent's verified 4x-QPF fixture; independently
   validate all result values/types before performance claims. Record actual
   commands, source settings and profile evidence, with private operational data
   excluded. No performance claim follows from source inspection alone.
