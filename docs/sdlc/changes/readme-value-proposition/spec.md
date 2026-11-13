# Specification

See [intent](intent.md) and [plan/checks](plan.md).

Change only the README value proposition and these change records. Cite the
recorded 604/651 (92.78%) curated PostgreSQL comparison with its 47 failures;
do not describe it as full conformance. Distinguish ordinary client access to
unmanaged HASHes from experimental managed snapshot-aware reads. Describe
managed ingestion, unmanaged keyed append and managed imports/refreshes with
their ownership and partial-write limits.

Scope O(1) to underlying fixed-size HASH field access, not complete SQL
statements. No evidence establishes general Redis-client latency parity or
superiority to every OLTP database for analytics; make neither claim. No runtime,
release, benchmark or supported-server changes.
