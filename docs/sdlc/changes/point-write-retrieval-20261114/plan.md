# Plan

[Intent](intent.md), [spec](spec.md). Date: November 14, 2026.

1. Managed agent profiles real four-column 1K mutations, then reduces actual
   serial visibility/staging round trips. Avoid assuming ready-first-poll index
   operations dominate complete write latency.
2. Point agent combines existing proof/data commands without new storage format
   or cached leases; explicit native command/permission/error checks remain.
3. Retrieval agent benchmarks narrow atomic existence/type+field reads before
   enabling them for live filtered scans; unchanged full-LOAD behavior stays
   separate until full SQL evidence supports changes.
4. Integrate isolated commits, review interactions and fallback/permission paths,
   run focused Software/race checks and appropriate broader suites.
5. Freeze builds/harness, run complete matched native matrices back-to-back with
   other heavy tasks paused, publish exact chart data and all regressions, finish
   ownership cleanup and open a PR. No merge authorization inferred.

Final implementation, complete native comparison, actual check sources/skips and
ownership cleanup are recorded in [validation](validation.md). Human approval
and merge remain separate.
