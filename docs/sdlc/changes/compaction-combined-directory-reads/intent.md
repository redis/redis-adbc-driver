# Intent

Date: November 13, 2026. Requestor: Albert Wong, delegated by the primary agent.
Baseline: merged main `0359c0d6` (same source tree as initial `34f13a21`). Status: implemented and locally verified; pending maintainer review.

Reduce resumed compaction directory-read round trips while retaining exact
immutable-source, row/byte, checkpoint and reclamation contracts. Separate
command-count evidence from end-to-end write performance. No 100M ClickBench
or storage-format change. See [spec](spec.md) and [plan](plan.md).
