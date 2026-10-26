# Implementation and evidence

See [intent](intent.md) and [spec](spec.md).

Implemented a pure scheduling gate, exact active-trial checkpoint controller, backup scheduling support, operator continuation, and post-performance API resume/preservation. The finisher requires final API evidence before early teardown and reports incomplete acceptance explicitly. Native code and timed binaries are unchanged.

Verification: four focused gate tests pass (unverified proposal rejection, quiescent checkpoint requirement, release not implying API completion, budget settlement not implying acceptance); Python compilation and git diff whitespace checks pass. Live handoff at 03:17:23Z stopped only the old supervisor, with the same isolated statement trial still active. Backup, restore, performance and resumed-API outcomes are pending; do not treat this implementation evidence as run acceptance.

Review: agent checked lifecycle, attempt preservation, quiescence and cleanup sequencing. Human PR review remains pending.
