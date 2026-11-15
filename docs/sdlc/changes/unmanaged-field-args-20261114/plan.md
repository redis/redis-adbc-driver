# Plan and outcome

[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Complete: inspect current native projection and pinned upstream source, record
   exact-source API requirements without duplicating Search #12334.
2. Complete: experiment with per-batch immutable field boxing and independent
   command args; remove separate-template singleton allocation after measurement.
3. Complete: add a production-batch microbenchmark and bounded opt-in sparse profile
   with independent source-image and native command-count checks; add an auditor
   that rejects missing/duplicate/corrupt matrices.
4. Complete in parent's exclusive slot: focused unit/race/live checks, matched
   batch microbench, sparse matrices and separate forward/reverse Go SQL pairs.
   Runtime/harness/binary provenance and every raw array remain available.
5. Complete: omit runtime changes after repeated four-client SQL median loss;
   archive both production experiments and retain harness/evidence only. Production
   reader and existing unit file equal `b904cf8d`. No more live work is needed for
   this evidence change; parent performs integration verification/review.

All live tests used verified Software 4×QPF; no ClickBench/external infrastructure
or upstream issue/comment. Fixtures closed/cleaned; exclusive slot released.
Human review remains pending.
