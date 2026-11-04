# Batched immutable-source validation

Date: 2026-11-04. Parent 3736b7c. Status: focused Software validation passes; broader gates open.
Full [parity](plan.md) remains incomplete.

The [read comparison](numeric-null-order-performance.md) leaves six cases
2.28x–3.83x slower than the pre-snapshot mutable baseline. Source inspection
found separate FT.INFO and ZCARD requests for each pinned source. A server-free
transport regression first reports six direct requests for three sources, with
no batches (root failure 0.779 s). This identifies avoidable requests; it does
not isolate all measured latency or establish a runtime speedup.

The implementation reserves per-statement segment proofs, then sends a bounded
per-table read pipeline containing every source's FT.INFO and directory ZCARD.
Descriptor validation bounds the source count and establishes canonical `{meta}`
index/directory names. The pipeline uses the explicitly selected primary and
Search's no-retry command wrapper, including for Cluster API connections. No
SQL, Arrow input, publication or uncertain mutation is replayed. Replica reads
are not introduced. Primary movement or unavailable replies refuse the statement;
a new statement acquires fresh proofs through the existing client routing.

Every previous readiness, indexed-image cardinality, exact directory cardinality,
indexing failure and physical-prefix check remains. Pipeline errors fail closed,
even when a transport hook has not set individual command errors. A proof becomes
visible only after validation completes. Concurrent preparations can reserve
different sources: each finishes its own batch before waiting, preventing a cycle
of waits. Existing inputs and private workspaces share their immutable proofs.
Changed descriptors and fresh statements cannot reuse another descriptor or
statement's result. Private outputs remain excluded until final sealing.

Waiting honors cancellation independently of the owner. Canceled owners publish
an error even when complete replies race with cancellation; canceled waiters do
not poison the owner's successful result. No format, ACL or ownership change.

## Evidence and remaining gates

- Final server-free focused matrix: 120 race pass events across ten repetitions,
  ten expected integration-test skips, zero failures; root 1.878 s. Covers warm
  and cold concurrent caches, one six-command batch for three sources, Search
  no-retry markers, denied metadata/directory, short directory, rebuilding index,
  incomplete images, indexing failures, wrong prefix, lost batch replies,
  fresh-statement recovery and owner/waiter cancellation.
- Complete current server-free package race checks: 969 pass events, 888 explicit
  integration skips, zero failures; root 51.129 s. CUDA has no applicable tests
  in this build and is skipped. These results do not establish Redis compatibility.
- Versioned native `make -C go build` passes on macOS ARM64; native client
  runtime and performance are not established by compilation.
- Real strict-TLS Redis Software 8.2.0-78.18 / Redis 8.6.2 with verified active
  effective 4x QPF and noeviction: 50 race pass events, zero test skips/failures;
  root 19.229 s. Includes three native published generations in one six-command
  validation batch, unchanged-descriptor cache reuse, changed-cardinality refusal,
  exact BIGINT/NULL/tombstone results, all numeric NULL-order guard families and
  the complete streaming fixture. CUDA has no applicable tests and is skipped.
- The [paired native comparison](validation-batch-performance.md) validates every
  result in four processes and records modest observed median speedups of
  1.02x–1.15x. This does not explain most remaining read overhead or prove parity.
  Current-protocol Cluster/failover remains pending; primary routing and
  server-free grouping do not establish Cluster recovery.

The full ordinary command started at 3736b7c before this implementation was
edited. Its root-package executable therefore exercises the preceding validation
code (production ac3c260). It stopped after 1,567 pass events, 263 skips and two
failure events (subtest plus parent), root 991.398 s, at
TestSQLStreamResults/materialized. All 25,000 rows were correct, but the fixture
assumed nullable NULLS FIRST always forces materialization. Its actual values
contain no NULLs, and the new absence proof safely enables ordered streaming.
The corrected fixture explicitly requests materialization for that case. Added
assertions independently validate every ID/label in the 25,000-row ordered stream
and exact materialized ordering/count when a visible NULL exists. Both pass in
the focused matrix above. The initial new-test compile caught an int/int64
argument mismatch; it was corrected before the successful run. No failed run is
passing evidence. The 143 root test families after this fixture pass in a separate continuation:
259 race pass events, five explicit skips, zero failures; root 216.360 s. Skips
are three opt-in acceptance/concurrency profiles, the existing ACL-sweep gate
and the server-pause timeout fixture. No skip establishes that capability.
Neither disjoint selections nor the preceding binary prove a complete current
ordinary-suite pass.

Author review checks visibility fences, source bounds, primary selection,
statement/workspace cache lifetime, synchronization and failure/cancellation.
It is not independent maintainer approval. Broader feature/performance parity,
current upstream integration, public resume, sustained compaction, restricted
readers and complete native/platform/recovery gates remain open.
