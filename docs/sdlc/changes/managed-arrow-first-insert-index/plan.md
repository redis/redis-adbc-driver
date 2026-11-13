# Plan and disposition: managed publication experiment

Date: 2026-11-13. Status: completed, rejected candidate; evidence only.
See [intent](intent.md), [audit](spec.md), [validation](validation.md).

1. Baseline current-source native call-size/writer controls and separate Go
   foreground/maintenance attribution. Complete: 16 native + six Go samples.
2. Audit eligibility, ownership, physical schema, unknown outcomes and query ACL
   fallback. Implement narrow runtime-only candidate and review it. Complete.
3. Run targeted correctness, actual restricted-principal and race regressions.
   Complete: final combined race selection 59 pass, zero skips/failures.
4. Commit candidate before matched native measurement. Complete: `f81b7a04`.
5. Run unchanged candidate matrix/diagnostics without tuning or discarding
   samples. Complete: 16 native + six Go samples, all validated, no retries.
6. Reject on default-call regressions; retain candidate as an unapplied patch
   and exact source/build hashes. Complete. Production code and invariant docs
   on this evidence branch match baseline.
7. Scope cleanup to the owned fixture, generated secrets and temporary
   principals; preserve all raw operational evidence privately. Record cleanup
   outcome in validation. Parent reviews/publishes evidence and independently
   decides the next optimization; no release is implied.
