# Intent: bounded batched Arrow construction

Date: November 13, 2026. Requestor: repository user; author: Codex.
Status: rejected for default adoption; evidence only. Baseline `24f654b7`,
original measured candidate `2caafbc3`. No production Go change is applied.

Wide direct projection already binds typed coercion/append callbacks. Investigate
bounded column staging and Arrow AppendValues to reduce per-cell builder work.
Preserve exact semantics and fallbacks. Adoption required consistent whole-path
benefit without a clear memory regression; isolated kernel speed was insufficient.

The candidate reduced conversion and several native time medians but increased
allocations and produced RSS distributions above baseline in two 4096-row cases.
Keep the exact patch/tests and all results; no eligibility retuning or selective
default. See [spec](spec.md), [plan](plan.md), [validation](validation.md) and
[complete evidence](../../../../benchmarks/table-modes/batched-arrow-2026-11-13/README.md).
