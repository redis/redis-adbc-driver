# Reduce remaining point/write/retrieval overhead

Date: November 14, 2026 (America/Los_Angeles). Requestor: repository user.
Author: Codex with three delegated agents. Production baseline: 0b21647831d13b0c10e652b7095515b0f5a74069.
Status: implementation; human review pending. PR pending.

After PR437, known-key reads remain about 3.3 ms, managed mutations about 41–68 ms and live
unmanaged aggregates remain dominated by retrieval. Improve exact proof/data
batching, version visibility lookup and filtered live-HASH retrieval without
weakening SQL/ownership/lifecycle guarantees. Root coordinates existing CI fix
PR438 and verified Software slots; performance is a separate reviewable change.

[Specification](spec.md), [plan](plan.md).

Final implementation, complete native comparison, actual check sources/skips and
ownership cleanup are recorded in [validation](validation.md). Human approval
and merge remain separate.

Implementation and evidence: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).
