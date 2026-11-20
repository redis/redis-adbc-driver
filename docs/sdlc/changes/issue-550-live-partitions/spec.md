# Contract

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex.
Source baseline: `845c95b4`. Status: implementation/verification in progress; no human approval.

[Intent](intent.md) · [Plan](plan.md) · [Verification](verification.md)

The statement option `adbc.redis.partition_source_mode=live_hash` enables deferred reads of one registered live table using the existing row-local partition SQL allowlist. The default remains managed. Live readers require `live_tables=true`; retained source execution and numeric balanced strategy remain rejected.

Version-6 descriptors assign SHA256(raw Redis key), first eight bytes interpreted big-endian modulo partition count. Each independent worker repeats bounded prefix SCAN, excludes nested keys, deduplicates its assigned keys and fetches authoritative HASH images before full SQL residual evaluation. No numeric row ID, saved SCAN cursor or persistent source inventory is used. Fixed-source rows have one bucket; concurrent application edits/expiry retain live semantics and retries need not reproduce values.

Each worker admits at most 100,000 distinct assigned keys, a 64 MiB logical key budget (key bytes plus 64 bytes), 1 MiB per key, 1,000,000 scanned entries and 4,096 SCAN pages. Requests target 128 HASHes/1 MiB key bytes. These are work/logical limits, not a decoder/RSS ceiling. Exceeding a budget is an explicit 53200 error, never successful truncation.

Descriptor admission reuses the existing bounded server partition group and child-pin lifetime. This protects admission metadata/lifetime, not application rows. Producer Close does not revoke descriptors; explicit release/expiry rejects new children; active child readers retain their own lease and bounded operation timeout.

Registration identity includes live replacement version, exact prefix/index/columns and type metadata. Workers independently authenticate, require the current registration, validate complete driver index definition/readiness before/after each source page and before returning each Arrow batch. A missing/replaced registration or changed/missing/unready index fails explicitly. Direct recreation with the same index identity/definition is outside the driver-owned-index contract and is not distinguishable as a new physical incarnation.

The driver-owned live index has no FILTER; byte-prefix/isRowKey defines collection membership. As with ordinary live full scans, all source HASHes in that collection are considered, including rows containing only unindexed/unknown fields. Full predicates run on fetched values; index lag or approximate index encodings cannot exclude valid fixed-source rows. Unexpected FILTERs are rejected. Sources, TTLs, unknown fields and keys are never changed by planning, reading, cancellation or cleanup.
