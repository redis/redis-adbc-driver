# Plan

Date: 2026-11-23. Status: implementation and synthetic verification.
See [intent](intent.md), [specification](spec.md), [verification](verification.md).

1. Retain the installed count/byte budgets; PR608 changes the count cap separately and is not included in this patch.
2. Share existing complete-count parsing/admission logic with a count-only proof.
3. Gate selective parallel admission on that proof; preserve full SQL predicates
   while forcing workers onto exact directory ranges.
4. Test both budget boundaries, later-source failure, warnings/corrupt replies,
   overflow and ambiguous errors, plus complete native mock range execution.
5. Keep the earlier broad-fact parallel revision stable. Native integration and
   selected query retries remain separate; full suites stay stopped.
