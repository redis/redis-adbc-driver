# Implementation plan

Date: 2026-11-09. Issue: [#335](https://github.com/redis/redis-adbc-driver/issues/335).
Status: implementation and focused recovery validation complete; broader
regression and PR delivery checks pending. See [validation](validation.md).

1. Audit current protocol-16 capture, mutation, publication, GC and native/ADBC
   execution lifecycles. Review saved branch 1a641d7f selectively.
2. Validate stable import row-ID reservation and bounded consumption, including
   source-order/missing-reservation/unknown-reply/ACL failures on current Software.
3. Implement independently retained ownership, bounded status records, exclusive
   takeover/renewal/fencing and reference-safe terminal cleanup, behind a new
   storage fence. Persist semantic dependencies and private index plans.
4. Wire explicit native/ADBC prepare/status/resume/abort for imports, SQL and
   Arrow without replay; fail closed for incomplete capture/runtime interruption.
5. Prove separate-process continuation and terminal outcomes across partial
   output/index work, cancellation, source/dependency mutation, expiry and GC.
6. Validate real Cluster routing, promotion, AOF/RDB recovery and performance/
   storage costs; review, deliver a checked PR and verify the issue's acceptance.

Do not claim completion after a prerequisite or simulated recovery alone.
See [intent](intent.md) and [spec](spec.md).
