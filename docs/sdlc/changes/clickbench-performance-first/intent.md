# ClickBench performance-first schedule

2026-10-21. Requested by Albert Wong; implemented by Codex.

Albert answered “Run the #406 performance comparison first” after backup and restore verification. The direct reply was independently read in chat 01a15fa9-519d-7f03-9439-1462f33208a3, message 01a16199-5510-7e81-8f27-53e56e5f5806. This changes scheduling, not correctness acceptance or resource expiry.

Preserve the active scan without changing its timing, checkpoint the current pair, complete the 48-shard backup and restore verification, then run #406 before resuming remaining supported API scans. See [spec](spec.md) and [plan](plan.md).
