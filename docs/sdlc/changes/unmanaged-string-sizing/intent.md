# Intent: evaluate unmanaged string byte accounting

Date: November 13, 2026. Requestor: repository user; author: Codex.
Status: evaluated and rejected as default; production unchanged.
Baseline `3213aa54`, with production equal to `5f759300`.

After the combined argument-allocation candidate regressed small sparse writes,
isolate only formatting already encoded strings in the unmanaged byte estimate.
Leave managed writes, original command-vector growth and per-cell field boxing
unchanged. Evaluate independent construction/sizing/combined phases and new
complete native SQL/Arrow/raw controls before adopting any default.

Isolated medians improved in all 16 input groups and fixed the earlier sparse
cache cost, but native Arrow four-writer median elapsed increased 5.8% with no
RSS benefit. Reject this narrower default too; preserve all 48 native and 336
isolated samples without assigning the native result to a specific mechanism.

See [contract](spec.md), [plan](plan.md) and [validation](validation.md).
