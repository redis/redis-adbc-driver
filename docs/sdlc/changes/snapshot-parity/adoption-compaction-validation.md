# Failed refresh checks during background compaction

Date: 2026-11-05. Production: 50d6b84, unchanged by this fixture correction.
Status: deterministic reproduction and repeated race checks pass; full ordinary
suite rerun required. See [intent](intent.md), [spec](spec.md), [plan](plan.md) and
[pre-copy indexing evidence](compaction-directory-index-design.md).

The first full ordinary run after the matched benchmark terminates with
`TestAdoptHashes` failing in 0.83 s at its global-root equality check. It reports
"failed refresh published" whenever the catalog root changes between observing
it and rejecting a typed import. That assumes no unrelated or maintenance
publication can occur. Faster background compaction makes that assumption
unreliable; a root change alone does not identify the publisher.

A new deterministic regression stops only its fixture-owned automatic controller,
creates a real eligible compaction job, then completes it through another leased
owner while adoption reads the application HASHes. It uses the existing client
pipeline hook, not a production callback, invented root mutation or concurrent
user-data write. The rejected refresh still returns its not-ready report with
Applied false. Requiring the catalog root to stay fixed fails in 0.45 s because
legitimate compaction published during planning.

The existing test now compares the complete published logical state instead:
serialized definition, identity/lineage, import origin and constraints; exact
Arrow schema; and every internal row ID, source key and public value. Only the
six physical layout fields that compaction may replace are excluded from the
metadata comparison: prefix, index name, prefix generation, overlay layout,
segments and physical segment count. The deterministic case additionally
requires that compaction actually changed the root. All previous adoption
checks, including original-HASH ownership and the rejected report, remain.
This catches row/schema/provenance/identity changes that root equality alone
could miss without mislabeling legitimate physical maintenance.

The corrected original adoption case and deterministic interleaving pass ten
race-enabled repetitions: 20 test events, no skips/failures, root 22.986 s.
The production library and its completed snapshot/native/benchmark results are
unchanged. This is not proof that the whole ordinary suite passes; its failed
attempt remains a failed attempt. The next complete run uses a reset owned
fixture after all focused processes are terminal.

Owned Redis Software 8.8.0, strict verified TLS, active/effective 4x QPF,
noeviction, three primaries behind its proxy; macOS ARM64 / Go 1.27.3. Author
review covers the real publication interleaving, native lease/ref ownership,
complete logical checks, source ownership and the distinction between root
identity and table contents. This is not maintainer approval or completion of
reader authorization, Cluster/recovery or performance parity.
