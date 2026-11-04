# Native retained capture fixtures

Date: 2026-11-04. Parent 6215f51. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

The preceding fresh 633-name ordinary continuation passes nine test events and
skips one coordinator helper, then fails
TestRetainedSourceCaptureExactLargeIDsAndMissingValues on an unpinned internal
catalog read (root 1.141 s). The old fixture also moved published HASHes and
rewrote metadata in place, and expected later DDL to invalidate immutable input.

The capture fixture now uses the production bounded source client with snapshot
hooks and one actual reader pin. Sparse IDs above 2^53 through MaxInt64 are
published with private images, tombstones, exact directories and native sequence
metadata. SQL ADD DEFAULT materializes 17 at those IDs. A separate read-only
metadata copy adds an absent virtual field with missing value 23 and an exact
MaxInt64 watermark, retaining decoder coverage without changing published data
or implying SQL produces that layout. Capture checks exact IDs, defaults, NULLs,
text, unchanged pinned metadata and successful recapture after later column DDL.
Fresh metadata independently observes the rename.

Application HASH, application-index and foreign-index modes use actual managed
copies. Internal capture and public retained replay verify binary original keys,
binary values, numeric values and complete Arrow schema/row coverage. External
HASHes and Search definitions remain unchanged. Multi-prefix filtered foreign
capture retains exact source membership. Row/key/byte budget refusals return no
partial capture and preserve source rows.

Legacy retained producers now use the shared explicitly disposable bootstrap
refusal fixture, with and without an existing Search index. Three failed opens
preserve the original descriptor, HASH and optional index, with no snapshot
catalog initialization or migration. Reader-only retained replay is unaffected.
The stronger existing-index refusal remains valid before the format check.

## Validation and review

Owned Redis Software with effective active 4x QPF and noeviction verified before
testing; mutating selections run sequentially. The retained control-persistence
declaration is supplied only after that management API verification.

- Initial restored capture selection: 10 race test events passed, no skips or
  failures; root 4.146 s.
- Final capture plus ordinary/retained legacy refusal selection: 16 race events
  passed, no skips/failures; root 2.994 s.
- Formatting and diff whitespace checks pass.

Reviewed private publication, pin/client cleanup order, exact sparse directory
membership, application ownership and failure-result assertions. The missing
value case is explicitly a virtual decoder fixture; legacy storage is refused
rather than migrated. No production code changes are required. This author
review is not independent maintainer approval.

Full ordinary/platform, restricted-reader production integration, current-protocol
Cluster failover/AOF/RDB recovery, process/input resumption, sustained compaction
throughput and comparative workload performance remain open. Prior complete
snapshot/native results remain tied to 6215f51. Disjoint selections are not a
complete ordinary-suite pass.
