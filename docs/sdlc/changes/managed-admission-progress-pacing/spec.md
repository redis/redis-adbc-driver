# Contract

After every rejected table-admission attempt, release the pin and wake compaction as before. Use 5/10/20/40/80/100 ms exponential backoff while fresh pins observe the same target latest physical prefix and segment count. If the next rejected attempt has a different target latest physical prefix or segment count, reset that wait to 5 ms. Only this delay changes. Target data identity is a scheduling hint, never a cached catalog, lease, permission or admission proof.

Preserve fresh snapshot acquisition, target resolution/full validation and existing server admission on every eligible attempt, immediate abort on unknown outcomes, full-lineage pin release before waiting, cancellation, no workspace/input consumption before admission and no execution replay. Unchanged-root stalls retain existing command pacing. Unrelated catalog publications do not change this target-local signal. Selective compaction can retain the latest prefix while reducing segment count, which resets pacing. Measure command amplification and throughput as well as tails. This provides more retry opportunities during observed target progress; it does not guarantee FIFO admission or starvation freedom.

[Intent](intent.md), [plan](plan.md), [validation](validation.md).

Outcome: this candidate contract was tested and rejected. It describes archived experimental source, not shipped runtime behavior; the active driver retains original bounded exponential pacing. [Evidence](../../../../benchmarks/managed-admission-pacing/report.md).
