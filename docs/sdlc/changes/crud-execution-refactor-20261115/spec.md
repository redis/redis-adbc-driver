# Specification: selected unmanaged known-key row projection

See [intent](intent.md) and [plan](plan.md).

## Selected runtime contract

For an ordinary unmanaged adopted key-row table, a planned known-`__key` row
retrieval may reuse the existing `unmanagedProjectionRead` instead of full
HGETALL transfer. This includes ordinary and streaming reads and UPDATE/DELETE
row matching that reaches the same scan path. The required projection must
include every selected, DML image, RETURNING, residual and hidden field required
by the existing scan iterator, including ordering/limit semantics. This changes
row-fetch transfer, not mutation/write-publication semantics, and does not
presume an INSERT/UPDATE/DELETE performance gain.

This helper establishes no new membership or authority. Retain fresh catalog,
index-definition, prefix/nested-key, lease and applicable permission checks.
Foreign/filter membership, unmanaged foreign tables, managed snapshots and Flex
retain their existing execution paths. Do not broaden the eligible planner
shapes merely to increase benchmark coverage.

Reuse the existing exact live reader's native outcomes and fallback contract.
Preserve NULL versus missing-HASH behavior, empty strings, exact int64/native
encodings, wrong types, binary keys, full predicates, cancellation/Close and
Arrow ownership. Its atomic full-HASH fallback must distinguish an all-NULL
selected projection from a missing HASH without combining inconsistent live
observations. Definite native permission refusals may take only the established
safe fallback; wrapped/joined or unknown outcomes remain terminal. Never replay
SQL, Arrow input or uncertain mutations. Unmanaged application resources remain
externally owned.

No managed admission/publication, unmanaged mutation semantics, new runtime
option, metadata routing cache, prepared-plan cache, GC policy or storage format
change is selected.

## Rejected metadata experiment

The descriptor-read prototype tested a bounded per-store physical owner hint,
then performed fresh declared-key root/bucket/object/native permission proofs.
Its final frozen candidate `4871e159889eba104c5eb668d0718a3d67a904b6` also contained
the separately evaluated unmanaged projection change; the screen is not a
cache-only ablation.

The predeclared requirement was at least 10% less pooled one-client point-read
p50 independently in managed and unmanaged modes. The complete point-only ABBA
screen measured 9.44% and 7.11% less elapsed time respectively, so both missed
the gate. Unmanaged four-client p95 also rose about 9.92%. Keep the complete
screen, raw controls, failed checkpoints and corrected prototype as unapplied
evidence. Do not infer a full CRUD/query gain from this point-only screen, weaken
the gate after observing the results, or ship a new flag for the rejected idea.

## Acceptance and remaining verification

- Use the unchanged complete typed-result and mutation final-state oracles for
  final selected-build native comparisons. Retain all warmups, slower/failing
  cases, one/four-client cases, raw Redis controls and exact build identities.
  A final result must identify the projection-only runtime, not the rejected
  prototype or an earlier source with additional metadata code.
- For projection, verify actual required-field HMGET selection and exclusion of
  registered-but-unselected and unregistered payloads. Measure narrow and wide
  application HASH layouts with exact physical types/values, source-state
  preservation, NUL coverage and complete result consumption. Synthetic value-
  byte oracles exclude field names/RESP/TLS and are not measured wire bytes.
  Do not claim the narrow case improves unless its matched observations do.
- Cover point and streaming paths, residual predicates, missing/wrongtype/all-
  NULL/native permission/composed errors, binary keys, foreign/Flex exclusions,
  cancellation and resource ownership. Run change-appropriate verified Software,
  race and snapshot suites; retain every skip and actual checkpoint.
- Diagnostic Go command/hook observations and instrumented native cursor spans
  remain separate from uninstrumented latency. A client hook is not a packet
  RTT; elapsed time is not server CPU. Native subspans overlap outer spans;
  independently pooled medians cannot be subtracted as a decomposition.
- All live work uses an exclusively scheduled Redis Software fixture created
  through `make -C go redis-up`, independently verified active/effective 4× QPF,
  six workers/mnp32/4-GiB/noeviction, trusted TLS/RESP3 and recorded resources.
  Build compared native libraries before timing and freeze code/harness hashes.
- Preserve original server/settings labels and all negative evidence. Verify
  owned cleanup, keep operational identifiers/credentials/licenses/logs private,
  and record actual final checks rather than infer human maintainer approval.

Final selected-code correctness and performance results remain pending until
recorded in the final validation/report artifacts.
