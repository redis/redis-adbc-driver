# Intent

Date: November 13, 2026. Requestor: repository user; implementation by Codex.
Source: `3213aa54` (production behavior matches `5f759300`). Candidate evaluated,
ready for parent review; no human approval recorded. Reduce repeated background compaction control round trips without
changing managed-table consistency, bounded copy work or recovery behavior.

The preceding attribution observed repeated control acquisition/release around
each bounded step; it did not isolate server CPU or admission wait. The earlier
early-index experiment was rejected and remains unapplied. See
[contract](spec.md), [plan](plan.md) and [validation](validation.md).
