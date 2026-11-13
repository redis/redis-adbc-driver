# Intent

Date: November 13, 2026. Requestor: Albert Wong, delegated performance follow-up.
Baseline: merged main `0359c0d6` (identical source to initial `34f13a21`).
Status: candidate measured; human acceptance of the memory/latency tradeoff pending.

Reduce retained memory from the larger 4096-row managed HASH pages introduced
in PR #408 while preserving their round-trip/latency benefit. Release consumed
raw reply references promptly without shrinking discovery or fetch pages.
Reduce repeated HMGET field-name boxing while preserving independent command
arguments, results and normal pipeline routing/hooks/errors.

Preserve exact values/NULLs, complete results, Arrow ownership and cancellation,
visibility/index/directory/pin proofs, and bounded prefetch. No storage, protocol,
managed-write or unmanaged-write changes; no 100M ClickBench work.
See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
