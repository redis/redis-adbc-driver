# Specification

2026-11-03. [Intent](intent.md), [plan](plan.md), [validation](validation.md).

- Register readers against the metadata key with WATCH/MULTI, serialized with
  refresh publication. Keep a physical generation and its allocated-ID bound
  for every reader. Repeated references in a statement use the same generation;
  subsequent statements in a script resolve newly published generations.
- Use renewable leases on the maintenance connection. Reuse the existing
  re-key lease timing/expiry machinery, with unique owners per execution.
  Metadata, membership and retirement keys all use the `{meta}` slot.
- Publication records the old physical generation as retired atomically with
  switching the metadata. It keeps old indexes/row prefixes registered while
  any reader lease is alive. Row/index deletion occurs separately afterward.
- Finish/release/cancel/connection close stop renewal and remove membership.
  Connection open and refresh retry collection after abandoned leases expire.
  Multiple collectors release a generation exactly once. Cleanup errors retain
  durable retirement metadata for retry.
- Protect normal indexed/range/parallel scans and retained source capture.
  A lost lease fails explicitly with SQLSTATE 55000. Destructive DDL, server
  failure, and mixed-version clients are outside the refresh-only guarantee.
- Do not add UNIQUE-index enforcement, delta refresh, general transactions or
  atomic input snapshots across tables.

Upgrade every client performing refresh before relying on the reader guarantee.
Open results can retain entire old generations; applications must release them.
