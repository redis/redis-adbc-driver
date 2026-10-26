# Specification

2026-10-21; baseline `96f75c1`; issue #363; author Codex.
See [intent](intent.md), [plan/evidence](plan.md).

- Use the official PostgreSQL ClickBench DDL and all 43 queries unchanged except
  schema qualification. Ingest field names follow unquoted identifier folding.
- Record each official query as supported or explicitly NOT_IMPLEMENTED. Only
  supported queries are timed; failures must never appear as successful timings.
- Label additional row-local scans/filters/expressions supplemental. These are
  not official ClickBench leaderboard queries.
- Use a dedicated task-owned local Redis and a fresh generated schema per run;
  clean up only that schema/table. Preserve the original benchmark runner's
  Cloud placement and Linux systemd isolation requirements.
- Wait for FT.INFO expected document count and indexing=0 after ingestion and
  layout changes before timing reads. Preserve failed attempts and never
  deduplicate benchmark output to hide a mismatch.
- Execute both methods in spawned workers. End-to-end time includes planning,
  child startup, connection setup, complete fetch and Arrow IPC writes. Parent
  exact-value/schema validation is outside timing. Rotate method order across
  repetitions; report median and all raw measurements. No cold-cache claim.
- Validate all values against typed source/DuckDB, using source ordinal for
  supplemental scans and row multisets for unordered official results. Validate
  partition schemas against planned and ordinary Arrow schemas.
- Record source/binary hashes and revisions, platform/package/server versions,
  row/column counts, cap and layout. Retain a one-tenth sparse and tail-skewed
  layout without renumbering internal IDs.
- Sample live coordinator-plus-worker RSS and Redis used_memory every 50ms;
  record lifetime per-worker peak RSS and Redis network deltas. These samples
  may miss short peaks; summed RSS double counts shared pages and includes
  coordinator source/oracle state. Network includes monitor traffic. No enforced
  per-query memory cap, isolated server-memory attribution or production claim.
- CI artifacts are evidence only for their exact source revision and runtime;
  platform packaging, TLS/ACL/Cloud gaps stay explicit.
