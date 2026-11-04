# Upstream feature integration with managed snapshots

Date: 2026-11-04. Merge parents: snapshot 36a9cdd and upstream f4d5b78.
Full [parity](plan.md) remains incomplete. This checkpoint integrates the fetched
upstream Arrow/ADBC contracts without reverting atomic managed storage.

## Combined behavior

- Live revision-3 descriptors accept a snapshot capability, supported output
  layouts, or both; revision 1 rejects either extension. Invalid capabilities or
  layouts reject independently. Managed readers still require a shared snapshot,
  even for layout-only descriptors. Layout conversion runs after attaching the
  underlying stream's pin; transport ownership follows the underlying reader.
  Producer/statement closure and table replacement do not change a produced set.
- Authoritative GetStatistics now returns standard column NULL counts alongside
  row counts, using one pinned catalog/schema/data generation. Exact directory
  merging resolves replacements/tombstones before bounded identity/deletion
  headers and field/explicit-NULL-marker existence probes. Arbitrary values do
  not cross the wire for counting. Defaults, empty values, NaN, denied probes and
  materialized results after pin release retain their precise behavior.
- The NULL scan retains a one-minute deadline, 4,096-column ceiling and two-million
  work-unit budget, charging physical directory entries plus two presence probes
  per visible row/column. Approximate-request statistics still use proven visible
  Search counts or the existing authoritative fallback and omit column counts.
  Both API modes retain the approximate classification. No quota is raised.
- Upstream Map/Union storage, nested extension normalization, dictionary IPC,
  explicit DISTINCT incremental execution, cancellable sorting and Arrow device
  boundary changes are incorporated. Tests below distinguish implemented paths
  from unverified device/platform behavior.
- SQL GRANT/REVOKE remains explicitly gated before management mutation. Upstream's
  static physical-prefix grants and read-only catalog rules cannot authorize
  immutable future generations or safely acquire protocol-15 reader pins.
  Configuration/parser/mock boundary code is retained; neither real provisioning
  nor a broader catalog grant is activated. [Reader isolation](reader-acl-design.md)
  and SQL SELECT grant parity remain unfinished. Documentation labels this gap.

Storage stays protocol 15 with no migration/disable switch. Managed external
copies require explicit refresh. Autocommit-only scope, noeviction and the
Active-Active exclusion remain. Partition descriptor revisions are separate from
storage protocol numbers. Managed rows still share the metadata hash slot.

## Actual validation

The merged upstream default uses the latest Software image. The owned exclusive
fixture was recreated and observed as **Redis 8.8.0**, three primaries, strict data
TLS, noeviction and active effective **4x QPF** verified by `make -C go redis-up`
and again before every Software selection. Historical 8.2/Redis 8.6.2 timings
remain labeled with their actual server; they are not 8.8 performance evidence.
Database-mutating selections ran sequentially. Owned resets verified the Compose
project/service, loopback endpoint and TLS before dropping fixture indexes/data.

- Final combined Software selection: **127 race pass events, one existing
  restricted-statistics ACL skip, zero failures; root 48.552 s**. Includes Map/Union
  Arrow inventory/IPC/independent replay, shared-generation partition lifetimes,
  output layouts, required DISTINCT lifecycle/full 25,000-ID results, all streaming
  fixture cases and statistics. A one-row layout stream survives producer close,
  table DROP, capability release and reclamation, then releases its child pin.
  Concurrent DROP/recreation preserves old column definitions and exact NULL
  counts while fresh statistics see replacement data. A one-megabyte value is
  counted through presence probes; denied probes return no partial result.
- Final server-free package race run: **1,032 pass events, 895 explicit integration
  skips, zero failures; root 54.995 s**. Descriptor combinations and the management
  mutation gate are covered. CUDA has no applicable tests in this host build and
  is skipped. These are not Redis or GPU compatibility results.
- Versioned `make -C go build` passes on macOS ARM64 with Go 1.27.3. Native C/ADBC
  acceptance across test_arrow_interop.py, test_followups_300_306.py,
  test_statistics.py and test_partitions.py: **113 pass, zero skips/failures,
  28.83 s**. Python 3.14.10, PyArrow 26.0.1 and driver manager 1.13.0. The run uses
  software-setup.sh --exec to pass the externally verified retained-control
  policy and explicit TLS CA. Independent process replay and worker death are
  included; a fresh query separately proves intervening writes took effect.
- Fixture unit checks: four QPF checks and five setup/exec checks pass. The first
  QPF invocation used an interpreter without redis-py; the configured validation
  environment passes. Python AST, Go formatting, document links and diff whitespace
  are checked. Source/library artifacts and operational logs remain private.

## Failed invocations and fixture corrections

No failed run is counted as complete validation. The initial Software selection
passes 87 events before a new fixture incorrectly tries a connection-level batch
option. It now sets the documented database-level option before opening workers.
The next selection finds a new DISTINCT assertion incorrectly expecting default
incremental behavior; the assertion now opts into stream_required and keeps the
original default-materialization case. Final complete selected results are above.

Native acceptance first stops after one pass because an old fixture expects a
failed create to leave an empty table. It now requires a missing descriptor and
separately proves rejected appends preserve exact seeded rows. Malformed Map/Union
input similarly must publish no descriptor. A second invocation passes 46 cases
before missing the externally verified retained-policy declaration; the corrected
setup child supplies it after policy verification. A third passes 62 before
completed parameterized live sets exhaust the 16-generation limit. Explicit public
release now revokes only capabilities returned to each test, including low-level
and subprocess producers. The generation limit and producer-close lifetime are
unchanged. Old retry/ALTER fixtures now compare the pinned generation and also
check fresh SQL state; they do not silently accept live mixed-generation results.
The corrected complete 113-case native run passes from a clean owned fixture.

## Review and remaining gates

Reviewed pin attachment before layout wrapping, descriptor failure/cleanup paths,
visibility before statistics, bounded presence-only reads, default/NULL semantics,
error classification, explicit test-owned capability release, source ownership,
no replay, no format activation and no management permission broadening. Author
review is not independent maintainer approval.

A fresh complete ordinary suite and complete snapshot suite on this merged source
remain required, as do the wider native/platform/hosted matrix, current-protocol
Cluster role/failover/AOF/RDB recovery, real restricted readers/SQL grants, public
process/input resume and sustained compaction. Real GPU/device hardware evidence
from upstream is historical, not a new snapshot result. Comparative benchmarks
must use current pinned sources and matched server settings; no feature-wide or
performance-parity completion claim follows from this selection.
