# README flow specification

Date: 2026-11-12. Base: `53b76248`. Requestor: Albert Wong. Author: Codex.
Status: implemented locally; not released.

- Put the quick start immediately after the concise purpose and benefit sections.
- Keep installation, Redis startup, sample loading, first queries and cleanup in order.
- Use the generated authenticated TLS environment for direct Python commands.
- Explain managed snapshots, driver-only writes and explicit refresh of imported copies.
- Move both full feature matrices and the extended sample SQL into linked documents.
- Retain existing top-level matrix anchors as summaries linking to the full tables.
- Link the existing integration index rather than repeat its full inventory.
- Retain analytics disclosure and opt-out, autocommit limits, experimental scope,
  release/source distinction and links to compatibility evidence.

Acceptance: local Markdown links and fragments resolve, all feature rows survive,
and the diff contains documentation changes only. Check commands against the
current startup script, Makefile and examples; no new server tests are required.

See [intent](intent.md) and [verification](plan.md).
