# Intent: exact aggregate execution

Date: November 13, 2026 (America/Los_Angeles). Source: ca685806.
Author: Codex. Requestor: repository user. Status: implemented and focused validation passed;
human review pending. [Spec](spec.md), [plan](plan.md).

The user requested performance improvements across OLTP and selective/aggregate
queries. The four-column scenario benchmark uses BIGINT inputs and explicit
BIGINT output casts. Output casts already preserve aggregate discovery; default
exact-mode BIGINT reducer eligibility instead rejects these inputs before the
existing runtime precision checks. Live unmanaged aggregates must keep current
HASH values and predicate validation, rather than trust mutable sorting vectors.

Enable runtime-proven immutable BIGINT reductions without weakening precision,
and reduce per-row map/allocation work for direct scalar/grouped aggregates.
No unsupported Redis distributions, storage format changes, weaker snapshot
semantics, new 100M load or implicit query/mutation replay are in scope.
