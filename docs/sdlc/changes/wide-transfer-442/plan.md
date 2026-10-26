# Implementation and verification

Date: 2026-10-22. Base: `aab9612`. Status: investigation complete; review pending.
See [intent](intent.md) and [specification](spec.md).

1. Build exact baseline; use task-owned containers and identical limits.
2. Add a reproducible cross-database runner with persistent/spawned readers,
   explicit batch/compression settings, raw trial persistence and full oracle
   comparison. Add a separate optional byte meter.
3. Screen wide HMGET command argument reuse, then confirm or revert production
   changes according to elapsed results. Preserve experiment patches.
4. Run wide/narrow, regular/public-partition controls and current correctness
   checks for any promoted production change. Verify benchmark validation
   rejects wrong values and duplicate multiplicity.
5. Retain benchmark identities, reports and architecture assessment; review
   correctness/lifecycle, security/ownership, and design/evidence per REVIEW.md.
6. Stop/remove only this task's disposable containers. Prepare a reviewable PR
   linked to #442, with remaining acceptance gaps explicit.

Completed: matched wide/narrow matrices, explicit-range control, separate byte
diagnostic, two failed screens, fresh baseline allocation/CPU profile, harness
regressions, evidence review and cleanup. No production change was promoted.
The PR is a benchmark/investigation deliverable; it does not close #442.
Specialized typed appending and bounded overlap still need measured prototypes;
the server-export assessment identifies required capabilities without selecting
a supported new API. See [validation](validation.md) and the
[results](../../../../benchmarks/wide-transfer/cross-database-2026-10-22/README.md).
