# Plan

1. Root review of source rationale and pure pacing unit contract before live measurement.
2. Run unit pacing checks and existing SQL/Arrow cancellation, unknown-reply, concurrent DML and full-lineage/no-replay tests on an exclusive verified Software 4× QPF slot.
3. Run matched longer baseline/candidate interleaved 1/4/8 writer trials with 288 total writes each, warmup, three rotated measured trials, complete result oracle, zero retries and all failures retained. Pair blocks B-warm/C-warm, B0/C0, C1/B1, B2/C2. Build both binaries before timing. Run held-target idle/unrelated-writer controls for each mode. Compare p50/p95/max, throughput, acquire/admission calls and total foreground/maintenance command amplification. Keep rejected evidence.
4. Select runtime change only if evidence supports the tradeoff; otherwise retain an experiment patch and measured rejection with unchanged production behavior.

[Intent](intent.md), [spec](spec.md), [validation](validation.md).

Completed: unit/lifecycle checks, below-trigger unrelated-publication controls, all paired288-write trials, full output validation and offline comparison. Rejected runtime on mixed/worse tails, lower throughput and command amplification. Preserved source/unit patch and all trial evidence; active runtime restored. No further experiment or native confirmation planned for this candidate.

Review: [PR #440](https://github.com/redis/redis-adbc-driver/pull/440).
