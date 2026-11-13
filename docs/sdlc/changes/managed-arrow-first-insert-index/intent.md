# Intent: measure managed publication fixed costs

Date: 2026-11-13. Requestor: repository user (performance work with subagents).
Status: experiment completed; candidate rejected, evidence only.

[Baseline attribution](validation.md) identified a second index-notification
pass over complete managed Arrow row images. Test whether creating an empty
private index before first insert can remove that pass while preserving all
publication guarantees. The [candidate](candidate.patch) was implemented and
passed correctness tests, but regressed the default 1,000-row native workload
and produced no larger-call gain. It is **not applied** on this branch.

The current driver and its documented deferred-index invariant remain intact.
No calls are coalesced and no storage/protocol, API, provider or default changes
are made. Preserve the measurement controls and negative result so future work
starts from current-source evidence rather than repeating this hypothesis.

See [specification/audit](spec.md), [plan](plan.md), [validation](validation.md)
and all sanitized [samples](results.json). No 100M ClickBench rerun was performed.
