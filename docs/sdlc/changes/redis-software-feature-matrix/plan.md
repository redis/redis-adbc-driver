# Plan and evidence: capability tracking and Redis Software matrix

Date: 2026-10-22. Author: Codex. Status: implemented; documentation checks passed.
Source reviewed: main `12f0d6904ebe94ddb7c198e7dba9b34469ed4f47`.

1. Inspected all 20 yellow/red Arrow rows, type conversion, ingestion, metadata,
   stream/partition/cancellation APIs, native bindings and existing tests/guides.
2. Searched all existing repository issues (197 returned) and existing labels.
   Created labeled issues #464–#481 with pinned source links, proposed scopes and
   acceptance checks. Added investigation comments to #269 and #457. Checked
   that every new issue exists, remains open and has a repository label.
3. Added issue links to every yellow/red Arrow matrix row.
4. Reviewed official Arrow specifications and Redis Software documentation;
   inspected protocol defaults, TLS roots, QPF setup, proxy/Cluster behavior,
   ACLs, Flex refusal and retained-result persistence/consistency contracts.
5. Added the Redis Software matrix with server-versus-driver scope and evidence
   limits; checked local Markdown links/anchors, table structure, issue coverage
   and `git diff --check`.

Key finding: physical schema normalization is not necessarily value loss.
Retained partition replay already supports broad SQL, but does not provide an
atomic source snapshot. Redis Active-Active supports Search with independently
maintained indexes; this is not evidence for safe driver multi-region metadata,
row-ID or concurrent-DDL semantics.

No runtime tests or benchmarks were run. Runtime evidence cited in existing
feature guides remains historical. Human review/approval is unrecorded.

See the [intent](intent.md) and [specification](spec.md).

Follow-up scope review (2026-10-22): added N/A for Flight service hosting, CDC,
GPU execution, persistence configuration, backup/DR, native messaging and server
administration. Kept device-buffer interchange, mTLS, replica routing,
Active-Active compatibility and missing SQL/type mappings as applicable driver
capabilities. Marked existing RBAC authentication/authorization integration
supported; provisioning roles remains a server task. Retained all 20 existing
issue links without closing or promising implementation of companion features.
Checked both legends, table structure, local links/anchors and whitespace.

Follow-up separation (2026-10-22): split replication (N/A, server-managed
synchronization/configuration) from automatic failover client recovery (partial,
connection/routing/retry limits). Replica read routing remains a separate
unsupported driver option. Checked the updated 24-row Redis matrix, tracking
links, local Markdown links/anchors and whitespace; no runtime tests run.

Failover classification correction (2026-10-22): marked client recovery green
for supported proxy/Cluster API connections through go-redis. Normal network
errors and ambiguous retries also affect official Redis clients and do not by
themselves establish incomplete support. Dedicated live-failover validation is
not recorded; no new runtime validation or uninterrupted replay is claimed.
Consulted official go-redis error/retry guidance; documentation checks passed.
