# Review

2026-10-21 America/Los_Angeles. Author: Codex. Review scope: follow-up to merged
#430, source `ff4963a`, under the three passes in [REVIEW.md](../../../../REVIEW.md).
This records agent review; it is not human maintainer approval.

## Correctness and lifecycle

Reproduced the Redis 8.6 single-node failure before changing foreign discovery.
NOCONTENT document IDs preserve NUL/non-UTF-8 key bytes, while aggregate
`LOAD @__key` can return NULL attributes. The replacement still requests at most
128 keys and fetches authoritative HASH fields. RESP2/RESP3 tests reject missing
or incorrectly typed identities, preserve empty/binary keys, and own mutable
response bytes. Full OSS Go tests and Cloud source-layout regressions pass.

The migration readiness correction probes complete HASH loads and exact physical
row IDs on every primary. Cached counts or sortable IDs cannot satisfy readiness
alone. Native assertions remain exact; no driver retry or snapshot guarantee was
introduced. Eight consecutive owned routing runs passed.

## Security and data ownership

Foreign discovery remains read-only and scoped to the index. Document-ID
sampling needs FT.SEARCH, already part of the documented read-only command set.
Application HASH/index ownership assertions remain intact. Independent workers
receive only their own credentials; Cloud minimal-ACL checks and namespace
cleanup pass. All paid resources and temporary ACL principals were deleted and
absence verified. Local private Cloud material was removed. Evidence was checked
for credential-bearing Redis URLs and private keys.

## Specification and evidence

#359/#360/#362 runtime work merged in #430. #363 requires final deployment,
packaging and scoped performance evidence. Cloud Pro, Software, local TLS/routing,
full native suites and exact synthetic/ClickBench reads are linked in
[evidence.md](evidence.md). Each measurement retains its own source/library
identity. Foreign sampling changed after the managed-table measurements and is
not exercised by those workloads. Final hosted compatibility and release workflows both passed on `ff4963a`;
their reports, exact library hashes and feature skips are retained separately.

No additional actionable finding remains from these passes. Source capture is
not atomic, logical resource credits are not RSS limits, live Search can be
incomplete while migrating, and the measured small ClickBench sample does not
establish production throughput. These boundaries remain explicit contracts.
