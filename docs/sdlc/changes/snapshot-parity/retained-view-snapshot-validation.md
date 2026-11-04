# Retained publication and pinned view validation

Date: 2026-11-04. Parent afccb87. Production remains protocol 15.
Full [parity](plan.md) remains incomplete.

## Reproduction and implementation

A fresh 640-name ordinary continuation passes three test events and skips three
helper/environment cases, then fails the cardinality child and parent of
TestRetainedPartitionAPIPrepublicationFailure (root 1.729 s). The old fixture
expects a source INSERT to invalidate an already captured result. Managed
coordinator snapshots correctly retain the original three rows.

The fixture now distinguishes actual cancellation from concurrent source changes.
INSERT, ALTER, view replacement, physical rekey and DROP/recreate must publish
exact original rows and Arrow schema. Fresh SQL independently checks changed
source values/definitions. After producer close, source removal and GC, two
reader-only replays must still return the original output. Cancellation must
return no schema/descriptors, the cancellation status, and no manifest, payload
or charged byte reservation. Rekey uses SQL and requires fresh physical prefix
and index; it never alters a published index in place.

Restoring those expectations exposes a production view failure: six events pass,
then the view child and parent fail with invalid retained view metadata (root
4.936 s). Retained final validation uses bounded GETRANGE, which was absent from
snapshot command key scoping. It reads the logical catalog key outside the
pinned immutable metadata value.

GETRANGE now scopes its key through the existing snapshot metadata resolver.
Bounds and typed reply handling remain unchanged. Unit coverage rejects a
catalog GETRANGE without a snapshot and preserves the bounded read arguments.
No ACL grants, metadata/control objects, storage version or replay policy change.
Native retained replay now reads a view, replaces its definition, verifies fresh
SQL sees new values, removes view/table, and checks original exact Arrow replay.

Public retained/coverage docs now describe one pinned managed generation and
managed external copies with explicit refresh. External concurrent capture is
not promoted to an atomic application-data snapshot. Historical wheel validation
and unsupported legacy storage remain explicit.

## Review

Reviewed command key positions, readonly admission, typed bounded replies and the
existing native metadata resolver. Only the key is scoped; retained namespace
payload reads remain outside the catalog namespace. Concurrent-source fixtures
use SQL publications and a snapshot-aware inspection client, preserve original
Arrow types/values, and retain explicit cancellation cleanup checks. This author
review is not independent maintainer approval.

## Verification

Database runs use owned Redis Software, effective active 4x QPF verified before
testing, noeviction and sequential mutating suites. Native library built through
make -C go build with the version linker flag.

- Focused retained publication/replay/cancellation and command-scoping race
  selection: 11 passed, zero skips/failures; root 6.944 s.
- Unit-only packages: 947 passed, 851 expected integration skips, zero failures;
  root 28.521 s. Skips do not prove compatibility.
- Full snapshot race selection: 260 passed, 21 explicit test skips, zero
  failures; root 350.878 s. Skips are nine Cluster API cases, six permission
  cases and six separately gated reader-model cases.
- Rebuilt native snapshot validation: 19 passed, zero skips/failures;
  19.077 s, including view replacement and exact Arrow replay after source removal.
- Formatting, Python compilation and diff whitespace pass.

Full ordinary/platform, restricted-reader production integration, current-protocol
Cluster failover/AOF/RDB recovery, process/input resumption, sustained compaction
throughput and comparative workload performance remain open. Disjoint passing
selections are not a complete ordinary-suite pass.
