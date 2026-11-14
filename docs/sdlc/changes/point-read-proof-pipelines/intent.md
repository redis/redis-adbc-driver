# Intent: reduce point-read control round trips

Date: November 14, 2026 (America/Los_Angeles). Author: Codex. Requestor:
repository user. Base: merged main 0b216478. Status: focused validation complete;
integrated/native validation and human review pending. [Spec](spec.md),
[plan](plan.md), [validation](validation.md).

The preceding improvements retained a substantial point-query gap. A fresh
instrumented Software/TLS profile on this base shows 13 foreground client round
trips per known-address read in both modes: 20 Redis commands managed and 19
unmanaged. Four lease-valid operations remain part of the execution/stream
protocol. Persistent execution pools already eliminate repeated TLS handshakes.

Reduce serial exchanges by batching independent existing proof/data reads. Do
not cache leases, metadata authorization, application index definitions or live
row values across statements. Preserve exact managed visibility, existing
unmanaged membership rules, Arrow ownership, cancellation, and buffered-stream
lease validation. No new storage protocol, SQL API or supported server is needed.

Implementation and evidence: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).
