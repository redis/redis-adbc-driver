# Intent

Date: 2026-11-12. Requestor: repository user, prioritizing both managed ingestion
and query speed. Base: `349f1e32` (includes unmanaged PR #404 and SQL admission #403).
Status: implemented and locally validated; see [validation](validation.md).
No release or maintainer approval claimed.

Reduce avoidable query control work and client write allocations while preserving
managed statement snapshots, atomic publication, exact values and durable outcomes.
See [contract](spec.md), [plan](plan.md).
