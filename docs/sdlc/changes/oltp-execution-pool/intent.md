# Intent: reuse drained SQL execution pools

Date: November 13, 2026 (America/Los_Angeles). Author: Codex. Requestor:
repository user. Base: ca685806. Status: focused validation complete; integration review pending.
[Specification](spec.md), [plan](plan.md). Human review pending.

The measured managed/unmanaged known-address point reads take about 8–9 ms while
redis-py takes about 0.255 ms on the recorded fixture. Source inspection shows
that persistent ADBC connections still construct and close a separate go-redis
execution client/socket pool for each SQL call. They also synchronously collect
retired manifests/metadata/rows before acquisition even with a live maintenance
worker. These are hypotheses for latency attribution until instrumented and
native comparisons complete.

Reuse at most one fully drained execution pool per connection and delegate
ordinary acquisition garbage collection to its existing live maintenance worker.
Keep each execution's context, cancellation, stream ownership and reader/writer
pins separate. Preserve exact row/version/index/metadata proofs, current catalog
binding, no replay, independent maintenance clients, and bounded Close.

No new API, storage format, supported server, snapshot weakening or claim that
these changes eliminate all OLTP overhead is intended. Known-key SQL already has
a direct row path; preserve newest-version/tombstone and unmanaged membership
checks rather than introduce a second bypass.
