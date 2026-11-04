# Plan

2026-11-03; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. Done: fetch public main; create `codex/issues-193-195` from `6d7ac40` rather
   than carry the divergent historical worktree's unrelated changes.
2. Done: isolate a one-page producer/consumer prototype behind `scanprefetch`;
   retain existing batch sizes, commands, routing, TLS and typed appenders.
3. Done: implement queue/cancellation/error/ownership tests and a deterministic
   mutation-after-discovery integration case; measure additional live heap.
4. Done: draft a versioned shard export contract and the requested no-go
   recommendation for production integration. This is not server-owner approval.
5. Done: validate both build configurations and the experimental race
   suite on an isolated TLS Redis Software fixture with verified active 4x QPF.
6. Done: verify existing AWS/S3 backup provenance and prepare a temporary,
   cost-bounded restoration. Do not mutate unrelated running instances. Keep
   operational identifiers, credentials, licenses and infrastructure private.
7. In progress: restore a verified compatible backup and independent typed 100K × 105
   reference, run one warm-up/five alternating trials for wide/narrow, regular/
   public partitions and one/four readers. Record all failures, elapsed ranges,
   actual first batch sizes, CPU, allocations, memory and both wire directions.
   Fully validate values and multiplicity outside timing. Promote only if the
   elapsed evidence supports it; otherwise retain the failed screen explicitly.
8. Done: record the requested no-go recommendation for #195 with the versioned
   contract and observed stock-server capability/precision diagnostic. No
   production API/storage change is selected. Server-owner approval remains a
   prerequisite to any future production integration, not an approval claimed
   by this client-side no-go investigation. The issue's feasible-prototype
   performance gate applies if a native capability later becomes available.
9. Pending: review changes against `REVIEW.md`, close issues only after their
   acceptance work is complete, and then create the requested PR. No premature
   `Closes` claim, issue closure or PR creation while these gates remain open.
