# Managed-copy live partition ownership

Date: 2026-11-04. Parent 03d442f. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

## Reproduction and change

The ordinary ownership test still expected adopted HASHes and foreign indexes to
be rejected, although imports now produce private managed copies. Replacing that
expectation with exact shared-generation replay exposes two production blockers:
foreign-schema copies fail the permanent-table guard, and an ordinary worker
cannot resolve a valid foreign-copy capability without enabling discovery.

Native layout eligibility now follows actual ownership flags rather than rejecting
the foreign schema. Temporary tables and live external layouts remain excluded
from live partition production. After validating and pinning a live capability,
ReadPartition permits resolution of its single captured foreign table on that
executor. It does not enable connection-wide discovery, import other sources,
change ordinary SQL admission, or bypass native managed ownership checks.
Reader-only API admission, ACLs, control objects and storage protocol are unchanged.

The three ownership modes (HASH adoption, application-index adoption and foreign
index discovery) produce and replay exact partitions across explicit refresh,
public copy retirement and GC. Tests check binary source keys and values, int64
values above 2^53, NULLs, shared generations, unchanged external HASH/index
definitions and metadata, rejected legacy descriptors, explicit release and
reclamation of private data. Application edits remain invisible until refresh.
The native case uses an ordinary worker with foreign discovery disabled, checks
ordinary SQL remains unavailable, and replays both old and refreshed generations.

Legacy-format refusal uses a separately gated disposable snapshot fixture. Failed
opens preserve the original HASH, descriptor and optional Search index; no
migration or implicit snapshot catalog publication is permitted.

Retained capture was inspected: both native snapshot capture and external capture
materialize isMem sources, which remain excluded from managed row-ID scans.

## Current evidence

All database runs use owned Redis Software with effective 4x QPF verified before
testing; mutating suites run sequentially. Native library built with make -C go
build, including its version linker flag.

| Check | Result |
|---|---|
| Initial positive ownership cases | Adopted/application copies pass; foreign copy rejected by permanent-table guard |
| Ordinary-worker foreign replay before executor-local admission | Fails with table not found |
| Final ownership, legacy refusal, native import policy and foreign discovery race selection | 9 passed, no skips/failures; root 4.935 s |
| Full TestSnapshot race selection, count=1, packages sequential | 259 passed, 21 skipped, no failures; root 304.149 s |
| Native snapshot validation including ordinary-worker foreign replay | 19 passed, no skips/failures; 18.860 s |
| Unit-only packages, count=1, packages sequential | 947 passed, 850 skipped, no failures; root 26.442 s |
| Formatting, Python compilation and diff whitespace | Passed |

Skipped integration cases are not compatibility evidence. These checks do not
prove full ordinary/platform parity, production restricted-reader ACL integration,
current-protocol Cluster failover/persistence recovery, resumable input, sustained
compaction or comparative workload performance. Those gates remain open.
